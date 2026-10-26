package integrationtests

import (
	"context"
	"errors"
	"net"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestRuntimeAcceptedWaitCancellationDoesNotRetract(t *testing.T) {
	for _, padded := range []bool{false, true} {
		name := "SendAndWait"
		if padded {
			name = "SendPaddedAndWait"
		}
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			t.Cleanup(func() { _ = b.Close() })
			local, peer := runtimeSessions(t)
			entered := make(chan struct{})
			c := runtimeConnection(t, &gatedIOConn{Conn: a, entered: entered, write: true}, local, dgproto.ConnectionConfig{OutboundQueue: 1})
			c.Start(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if padded {
					result <- c.SendPaddedAndWait(ctx, dgproto.Ack{Sequences: []uint64{11}}, 7)
				} else {
					result <- c.SendAndWait(ctx, dgproto.Ack{Sequences: []uint64{11}})
				}
			}()
			// Actual socket Write means acceptance AND serialization have occurred.
			// net.Pipe holds the complete write until the peer starts reading.
			receiveBounded(t, entered)
			cancel()
			if err := receiveBounded(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("accepted cancel = %v", err)
			}
			if c.Err() != nil {
				t.Fatalf("caller cancellation killed runtime: %v", c.Err())
			}
			tr := dgproto.NewTCPTransport(b)
			first := readRuntimeAck(t, tr, peer, 11)
			if padded && len(first.Padding) != 7 {
				t.Fatalf("padding length = %d", len(first.Padding))
			}
			next := make(chan error, 1)
			go func() { next <- c.SendAndWait(context.Background(), dgproto.Ack{Sequences: []uint64{12}}) }()
			second := readRuntimeAck(t, tr, peer, 12)
			if second.Header.Sequence != first.Header.Sequence+1 {
				t.Fatal("canceled message retried or sequence skipped")
			}
			if err := receiveBounded(t, next); err != nil {
				t.Fatal(err)
			}
			if c.Err() != nil {
				t.Fatalf("runtime died after next write: %v", c.Err())
			}
		})
	}
}

func TestRuntimeQueueCancellationBeforeAdmissionHasNoTraffic(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = b.Close() })
	local, peer := runtimeSessions(t)
	entered := make(chan struct{})
	c := runtimeConnection(t, &gatedIOConn{Conn: a, entered: entered, write: true}, local, dgproto.ConnectionConfig{OutboundQueue: 1})
	c.Start(context.Background())
	if err := c.Send(dgproto.Ack{Sequences: []uint64{1}}); err != nil {
		t.Fatal(err)
	}
	receiveBounded(t, entered)
	if err := c.Send(dgproto.Ack{Sequences: []uint64{2}}); err != nil {
		t.Fatal(err)
	}
	if err := c.TrySend(dgproto.Ack{Sequences: []uint64{99}}); !errors.Is(err, dgproto.ErrOutboundQueueFull) {
		t.Fatalf("queue not full: %v", err)
	}
	for _, pre := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		observed := make(chan struct{})
		waiting := &observingContext{Context: ctx, observed: observed}
		if pre {
			cancel()
		}
		result := make(chan error, 1)
		go func() { result <- c.SendPaddedAndWait(waiting, dgproto.Ack{Sequences: []uint64{99}}, 3) }()
		if !pre {
			receiveBounded(t, observed)
			cancel()
		}
		if err := receiveBounded(t, result); !errors.Is(err, context.Canceled) {
			t.Fatalf("pre=%t: %v", pre, err)
		}
		cancel()
	}
	tr := dgproto.NewTCPTransport(b)
	readRuntimeAck(t, tr, peer, 1)
	readRuntimeAck(t, tr, peer, 2)
	result := make(chan error, 1)
	go func() { result <- c.SendAndWait(context.Background(), dgproto.Ack{Sequences: []uint64{3}}) }()
	frame := readRuntimeAck(t, tr, peer, 3)
	if frame.Header.Sequence != 3 {
		t.Fatalf("canceled work consumed sequence: %d", frame.Header.Sequence)
	}
	if err := receiveBounded(t, result); err != nil {
		t.Fatal(err)
	}
}
