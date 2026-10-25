package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// gatedIOConn signals entry into the actual I/O, not a scheduling guess.
type gatedIOConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
	write   bool
}

func (c *gatedIOConn) Read(p []byte) (int, error) {
	if !c.write {
		c.once.Do(func() { close(c.entered) })
	}
	return c.Conn.Read(p)
}
func (c *gatedIOConn) Write(p []byte) (int, error) {
	if c.write {
		c.once.Do(func() { close(c.entered) })
	}
	return c.Conn.Write(p)
}

func TestHardeningTCPLaneCancellation(t *testing.T) {
	for _, write := range []bool{false, true} {
		name := "read"
		if write {
			name = "write"
		}
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			entered := make(chan struct{})
			tr := NewTCPTransport(&gatedIOConn{Conn: a, entered: entered, write: write})
			frame := testTransportFrame(t, 1, []byte("test"))
			op := func(ctx context.Context) error {
				if write {
					return tr.WriteFrame(ctx, frame)
				}
				_, err := tr.ReadFrame(ctx)
				return err
			}
			firstCtx, cancelFirst := context.WithCancel(context.Background())
			defer cancelFirst()
			first := make(chan error, 1)
			go func() { first <- op(firstCtx) }()
			receiveBounded(t, entered)
			// A pre-canceled queued call must finish while the first still owns its lane.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			second := make(chan error, 1)
			go func() { second <- op(ctx) }()
			select {
			case err := <-second:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("queued cancellation = %v", err)
				}
			case <-time.After(connectionTestTimeout):
				cancelFirst()
				receiveBounded(t, first)
				receiveBounded(t, second)
				t.Fatal("canceled call waited behind occupied lane")
			}
			cancelFirst()
			if err := receiveBounded(t, first); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			// Cancellation must not install a deadline after the next owner begins.
			nextCtx, cancelNext := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancelNext()
			peer := make(chan error, 1)
			go func() {
				if write {
					_, err := NewTCPTransport(b).ReadFrame(nextCtx)
					peer <- err
				} else {
					peer <- NewTCPTransport(b).WriteFrame(nextCtx, frame)
				}
			}()
			if err := op(nextCtx); err != nil {
				t.Fatalf("next operation poisoned: %v", err)
			}
			if err := receiveBounded(t, peer); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHardeningPendingRekeyOwned(t *testing.T) {
	for _, part := range []string{"payload", "padding"} {
		t.Run(part, func(t *testing.T) {
			s, _ := testSessions(t)
			s.rekeyFrameLimit = 1
			if _, err := s.Send(PingPong{Nonce: 1}, 0); err != nil {
				t.Fatal(err)
			}
			frame, err := s.Send(PingPong{Nonce: 2}, 8)
			if err != nil {
				t.Fatal(err)
			}
			original := frame
			original.Payload = append([]byte(nil), frame.Payload...)
			original.Padding = append([]byte(nil), frame.Padding...)
			if part == "payload" {
				frame.Payload[0] ^= 1
			} else {
				frame.Padding[0] ^= 1
			}
			if err := s.MarkRekeySent(frame); !errors.Is(err, ErrRekeyPending) {
				t.Fatalf("mutated frame accepted: %v", err)
			}
			if err := s.MarkRekeySent(original); err != nil {
				t.Fatalf("original rejected: %v", err)
			}
		})
	}
}

// observingContext reports when a waiter has checked cancellation and is about
// to select on Done. This makes cancellation during lane acquisition bounded.
type observingContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestHardeningTCPCancelDuringLaneWait(t *testing.T) {
	for _, write := range []bool{false, true} {
		name := "read"
		if write {
			name = "write"
		}
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			entered := make(chan struct{})
			tr := NewTCPTransport(&gatedIOConn{Conn: a, entered: entered, write: write})
			frame := testTransportFrame(t, 1, nil)
			op := func(ctx context.Context) error {
				if write {
					return tr.WriteFrame(ctx, frame)
				}
				_, err := tr.ReadFrame(ctx)
				return err
			}
			firstCtx, cancelFirst := context.WithCancel(context.Background())
			defer cancelFirst()
			first := make(chan error, 1)
			go func() { first <- op(firstCtx) }()
			receiveBounded(t, entered)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := make(chan struct{})
			waiting := &observingContext{Context: ctx, observed: observed}
			result := make(chan error, 1)
			go func() { result <- op(waiting) }()
			receiveBounded(t, observed)
			cancel()
			if err := receiveBounded(t, result); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			cancelFirst()
			receiveBounded(t, first)
		})
	}
}
