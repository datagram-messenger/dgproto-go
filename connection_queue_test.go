package dgproto

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestConnectionOutboundQueueBackpressure(t *testing.T) {
	localConn, peerConn := net.Pipe()
	defer peerConn.Close()
	localSession, _ := testSessions(t)
	connection := NewConnection(NewTCPTransport(localConn), localSession, ConnectionConfig{OutboundQueue: 2})
	defer connection.Close()

	if err := connection.Send(PingPong{Nonce: 1}); err != nil {
		t.Fatal(err)
	}
	if err := connection.Send(PingPong{Nonce: 2}); err != nil {
		t.Fatal(err)
	}
	if err := connection.Send(PingPong{Nonce: 3}); !errors.Is(err, ErrOutboundQueueFull) {
		t.Fatalf("error = %v, want %v", err, ErrOutboundQueueFull)
	}
}

func TestConnectionControlQueueIndependentOfApplicationBackpressure(t *testing.T) {
	localConn, peerConn := net.Pipe()
	defer peerConn.Close()
	localSession, _ := testSessions(t)
	connection := NewConnection(NewTCPTransport(localConn), localSession, ConnectionConfig{OutboundQueue: 1})
	defer connection.Close()

	if err := connection.Send(PingPong{Nonce: 1}); err != nil {
		t.Fatal(err)
	}
	if err := connection.enqueueControl(PingPong{IsResponse: true, Nonce: 2}, false); err != nil {
		t.Fatalf("control enqueue with full application queue: %v", err)
	}
	if connection.closing.Load() {
		t.Fatal("control backpressure closed the connection")
	}
}

func TestHardeningStoppedQueueDisposal(t *testing.T) {
	c, _, _, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	completion := make(chan error, 1)
	if err := c.enqueue(context.Background(), queuedMessage{message: PingPong{Nonce: 1}, completion: completion}, false); err != nil {
		t.Fatal(err)
	}
	if err := c.enqueueControl(PingPong{Nonce: 2}, false); err != nil {
		t.Fatal(err)
	}
	c.handlerQueue <- &EncryptedData{Fields: []TLV{{Value: []byte("synthetic")}}}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if len(c.outbound) != 0 || len(c.control) != 0 || len(c.handlerQueue) != 0 {
		t.Fatalf("terminal queues retained references: outbound=%d control=%d handler=%d", len(c.outbound), len(c.control), len(c.handlerQueue))
	}
	if err := receiveBounded(t, completion); !errors.Is(err, ErrConnectionClosed) {
		t.Fatal(err)
	}
	select {
	case <-completion:
		t.Fatal("duplicate write completion")
	default:
	}
}

func TestHardeningProducerCloseStress(t *testing.T) {
	for range 40 {
		c, _, _, cleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 4})
		const producers = 24
		start := make(chan struct{})
		results := make(chan error, producers)
		completions := make([]chan error, producers)
		for i := range producers {
			completions[i] = make(chan error, 1)
			go func(i int) {
				<-start
				results <- c.enqueue(context.Background(), queuedMessage{message: PingPong{Nonce: uint64(i)}, completion: completions[i]}, true)
			}(i)
		}
		closed := make(chan error, 1)
		go func() { <-start; closed <- c.Close() }()
		close(start)
		receiveBounded(t, closed)
		accepted := 0
		for range producers {
			err := receiveBounded(t, results)
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrConnectionClosed) {
				t.Fatal(err)
			}
		}
		completed := 0
		for _, completion := range completions {
			select {
			case err := <-completion:
				completed++
				if !errors.Is(err, ErrConnectionClosed) {
					t.Fatal(err)
				}
			default:
			}
			select {
			case <-completion:
				t.Fatal("duplicate completion")
			default:
			}
		}
		if accepted != completed {
			t.Fatalf("accepted=%d completed=%d", accepted, completed)
		}
		if len(c.outbound) != 0 || len(c.control) != 0 || len(c.closeRequest) != 0 {
			t.Fatal("terminal queue references retained")
		}
		cleanup()
	}
}

func TestHardeningRunningWriteCompletionDisposal(t *testing.T) {
	for range 20 {
		a, b := net.Pipe()
		entered := make(chan struct{})
		session, _ := testSessions(t)
		c := NewConnection(NewTCPTransport(&gatedIOConn{Conn: a, entered: entered, write: true}), session, ConnectionConfig{OutboundQueue: 4})
		c.Start(context.Background())
		first := make(chan error, 1)
		if err := c.enqueue(context.Background(), queuedMessage{message: PingPong{}, completion: first}, true); err != nil {
			t.Fatal(err)
		}
		receiveBounded(t, entered)
		const producers = 16
		start := make(chan struct{})
		results := make(chan error, producers)
		completions := make([]chan error, producers)
		for i := range producers {
			completions[i] = make(chan error, 1)
			go func(i int) {
				<-start
				results <- c.enqueue(context.Background(), queuedMessage{message: PingPong{}, completion: completions[i]}, true)
			}(i)
		}
		aborted := make(chan struct{})
		go func() { <-start; c.abort(ErrConnectionClosed); close(aborted) }()
		close(start)
		receiveBounded(t, aborted)
		waitConnection(t, c)
		if err := receiveBounded(t, first); err == nil {
			t.Fatal("stalled in-flight write succeeded")
		}
		accepted := 0
		for range producers {
			err := receiveBounded(t, results)
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrConnectionClosed) {
				t.Fatal(err)
			}
		}
		completed := 0
		for _, ch := range completions {
			select {
			case err := <-ch:
				completed++
				if err == nil {
					t.Fatal("pending write succeeded")
				}
			default:
			}
			select {
			case <-ch:
				t.Fatal("duplicate completion")
			default:
			}
		}
		if accepted != completed {
			t.Fatalf("accepted=%d completed=%d", accepted, completed)
		}
		select {
		case <-first:
			t.Fatal("duplicate in-flight completion")
		default:
		}
		if len(c.outbound) != 0 || len(c.control) != 0 || len(c.closeRequest) != 0 {
			t.Fatal("running terminal queues retained references")
		}
		_ = b.Close()
		_ = c.Close()
	}
}
