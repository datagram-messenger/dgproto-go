package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConnectionPreCanceledStart(t *testing.T) {
	for _, peerClosed := range []bool{false, true} {
		t.Run(map[bool]string{false: "open peer", true: "closed peer"}[peerClosed], func(t *testing.T) {
			local, peer := net.Pipe()
			defer peer.Close()
			if peerClosed {
				_ = peer.Close()
			}
			session, _ := testSessions(t)
			var calls atomic.Int32
			c := NewConnection(NewTCPTransport(local), session, ConnectionConfig{Handler: func(context.Context, *Connection, any) error {
				calls.Add(1)
				return nil
			}})
			defer c.Close()
			completion := make(chan error, 1)
			c.outbound <- queuedMessage{message: PingPong{Nonce: 1}, completion: completion}
			c.handlerQueue <- &EncryptedData{}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c.Start(ctx)
			// A pre-canceled Start must finish synchronously without runtime dispatch.
			select {
			case <-c.Done():
			default:
				t.Error("Done not closed before Start returned")
			}
			waitConnection(t, c)
			if c.started.Load() || calls.Load() != 0 {
				t.Errorf("runtime started=%v handler calls=%d", c.started.Load(), calls.Load())
			}
			if !errors.Is(c.Err(), context.Canceled) {
				t.Errorf("cause=%v, want cancellation", c.Err())
			}
			select {
			case err := <-completion:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("queued completion=%v", err)
				}
			default:
				t.Error("queued completion not disposed")
			}
			if len(c.outbound) != 0 || len(c.handlerQueue) != 0 {
				t.Error("terminal queues not disposed")
			}
			var wg sync.WaitGroup
			for range 16 {
				wg.Go(func() { c.Start(context.Background()); _ = c.Close() })
			}
			wg.Wait()
			if !errors.Is(c.Err(), context.Canceled) || c.started.Load() {
				t.Errorf("repeated lifecycle changed state: %v", c.Err())
			}
		})
	}
}

func TestConnectionPreCanceledConcurrentStartClose(t *testing.T) {
	for range 64 {
		local, peer := net.Pipe()
		session, _ := testSessions(t)
		c := NewConnection(NewTCPTransport(local), session, ConnectionConfig{})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		gate := make(chan struct{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() { <-gate; c.Start(ctx) })
			wg.Go(func() { <-gate; _ = c.Close() })
		}
		close(gate)
		wg.Wait()
		waitConnection(t, c)
		_ = peer.Close()
		if c.started.Load() || (!errors.Is(c.Err(), context.Canceled) && !errors.Is(c.Err(), ErrConnectionClosed)) {
			t.Fatalf("started=%v cause=%v", c.started.Load(), c.Err())
		}
	}
}

func TestConnectionCancellationBeforePeerEOF(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	session, _ := testSessions(t)
	c := NewConnection(NewTCPTransport(local), session, ConnectionConfig{})
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx)
	peerDone := make(chan error, 1)
	go func() {
		frame, err := NewTCPTransport(peer).ReadFrame(context.Background())
		if err == nil && frame.Header.MessageType != MessageTypeSessionClose {
			err = errors.New("expected SessionClose")
		}
		_ = peer.Close()
		peerDone <- err
	}()
	cancel()
	waitConnection(t, c)
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatalf("cause=%v, want cancellation before peer EOF", c.Err())
	}
}
