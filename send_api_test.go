package dgproto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"testing/synctest"
	"time"
)

func TestSendContextFullQueueCancellationAndFIFOAdmission(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	session, _ := testSessions(t)
	connection := NewConnection(NewTCPTransport(local), session, ConnectionConfig{OutboundQueue: 1})
	defer connection.Close()
	if err := connection.Send(PingPong{Nonce: 1}); err != nil {
		t.Fatal(err)
	}

	pre, cancelPre := context.WithCancel(context.Background())
	cancelPre()
	if err := connection.SendContext(pre, PingPong{Nonce: 2}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel = %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { second <- connection.SendContext(ctx2, PingPong{Nonce: 2}) }()
	select {
	case err := <-second:
		t.Fatalf("send returned before capacity: %v", err)
	default:
	}
	cancel2()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-cancel = %v", err)
	}

	third := make(chan error, 1)
	go func() { third <- connection.SendContext(context.Background(), PingPong{Nonce: 3}) }()
	select {
	case err := <-third:
		t.Fatalf("third returned before capacity: %v", err)
	default:
	}
	first := <-connection.outbound
	if first.message.(PingPong).Nonce != 1 {
		t.Fatal("first item reordered")
	}
	if err := <-third; err != nil {
		t.Fatal(err)
	}
	last := <-connection.outbound
	if last.message.(PingPong).Nonce != 3 {
		t.Fatal("FIFO admission violated")
	}
}

func TestSendAndWaitCompletesAfterWriteAndPropagatesFailure(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 1})
	defer cleanup()
	connection.Start(context.Background())
	result := make(chan error, 1)
	go func() { result <- connection.SendAndWait(context.Background(), Ack{Sequences: []uint64{7}}) }()
	select {
	case err := <-result:
		t.Fatalf("completed before peer read: %v", err)
	default:
	}
	message := readConnectionMessage(t, peerTransport, peerSession)
	if got := message.(*Ack).Sequences; !reflect.DeepEqual(got, []uint64{7}) {
		t.Fatalf("ack = %v", got)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}

	failed, _, failedPeer, failedCleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 1})
	defer failedCleanup()
	failed.Start(context.Background())
	_ = failedPeer.Close()
	if err := failed.SendAndWait(context.Background(), Ack{Sequences: []uint64{8}}); err == nil {
		t.Fatal("write failure was lost")
	}
	waitConnection(t, failed)
}

// Hold the successful transport return so the handler can terminate the runtime
// after peer receipt but before writeLoop publishes this item's completion.
// This models a writer descheduled immediately after the underlying write.
type heldWriteCompletionConn struct {
	net.Conn
	written chan struct{}
	release chan struct{}
}

func (conn *heldWriteCompletionConn) Write(data []byte) (int, error) {
	n, err := conn.Conn.Write(data)
	close(conn.written)
	<-conn.release
	return n, err
}

func TestSendAndWaitHandlerFailureBeforeWriteCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("handler rejected reply before write completion")
		connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
			Handler: func(context.Context, *Connection, any) error { return cause },
		})
		defer cleanup()
		transport := &heldWriteCompletionConn{
			Conn:    connection.transport.conn,
			written: make(chan struct{}),
			release: make(chan struct{}),
		}
		connection.transport.conn = transport
		defer close(transport.release)
		connection.Start(context.Background())
		result := make(chan error, 1)
		go func() { result <- connection.SendAndWait(context.Background(), Ack{Sequences: []uint64{7}}) }()
		message := readConnectionMessage(t, peerTransport, peerSession)
		if got := message.(*Ack).Sequences; !reflect.DeepEqual(got, []uint64{7}) {
			t.Fatalf("ack = %v", got)
		}
		<-transport.written
		synctest.Wait()
		select {
		case err := <-result:
			t.Fatalf("completed before transport returned: %v", err)
		default:
		}
		writeConnectionMessage(t, peerTransport, peerSession, Ack{Sequences: []uint64{1}})
		if err := receiveBounded(t, result); !errors.Is(err, cause) {
			t.Fatalf("pending write result = %v, want handler cause", err)
		}
		if !errors.Is(connection.Err(), cause) {
			t.Fatalf("terminal cause = %v", connection.Err())
		}
		// The send waiter must not depend on the stalled writer exiting.
		select {
		case <-connection.Done():
			t.Fatal("runtime completed while transport write was held")
		default:
		}
	})
}

func TestSendAndWaitCloseRaceCompletesWithoutLeak(t *testing.T) {
	connection, _, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 8})
	defer cleanup()
	connection.Start(context.Background())
	results := make(chan error, 8)
	for i := range cap(results) {
		go func(n int) {
			results <- connection.SendAndWait(context.Background(), Ack{Sequences: []uint64{uint64(n + 1)}})
		}(i)
	}
	_ = peerTransport.Close()
	deadline := time.After(connectionTestTimeout)
	for range cap(results) {
		select {
		case <-results:
		case <-deadline:
			t.Fatal("completion leaked during close race")
		}
	}
	waitConnection(t, connection)
}

func TestCompleteSignalsExactlyOnce(t *testing.T) {
	completion := make(chan error, 2)
	item := queuedMessage{completion: completion}
	complete(item, ioTestError{})
	if err := <-completion; err == nil {
		t.Fatal("completion error missing")
	}
	select {
	case <-completion:
		t.Fatal("completion signaled twice")
	default:
	}
}

func TestSendContextPreCanceledWithAvailableCapacity(t *testing.T) {
	for _, waitForWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("waitForWrite=%t", waitForWrite), func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			session, _ := testSessions(t)
			connection := NewConnection(NewTCPTransport(local), session, ConnectionConfig{OutboundQueue: 1})
			defer connection.Close()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			for range 64 {
				var err error
				if waitForWrite {
					err = connection.SendAndWait(ctx, Ack{Sequences: []uint64{1}})
				} else {
					err = connection.SendContext(ctx, Ack{Sequences: []uint64{1}})
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("pre-canceled send = %v", err)
				}
				if len(connection.outbound) != 0 {
					t.Fatal("pre-canceled send enqueued a message")
				}
			}
		})
	}
}

type ioTestError struct{}

func (ioTestError) Error() string { return "write failed" }
