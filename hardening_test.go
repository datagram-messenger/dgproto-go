package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
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

func TestHardeningAdmissionSocketOwnership(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "Close"
		if abort {
			name = "Abort"
		}
		t.Run(name, func(t *testing.T) {
			config := validServerConfig(t)
			entered := make(chan *Connection, 1)
			release := make(chan struct{})
			config.Admission = func(_ context.Context, c *Connection, _ AdmissionInfo) error { entered <- c; <-release; return nil }
			s, err := NewServer(config)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			serve := make(chan error, 1)
			go func() { serve <- s.Serve(listener) }()
			// Cleanup releases the deliberately noncooperative hook before waiting.
			defer func() { close(release); _ = s.Abort(); _ = s.Close(); receiveBounded(t, serve) }()
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			client, _, err := DialContext(ctx, listener.Addr().String(), ClientConfig{StaticKey: generateTestStaticKey(t), ServerStatic: config.StaticKey.Public()})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			receiveBounded(t, entered)
			closed := make(chan error, 1)
			if abort {
				closed <- s.Abort()
			} else {
				go func() { closed <- s.Close() }()
			}
			readCtx, readCancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer readCancel()
			_, err = client.transport.ReadFrame(readCtx)
			if errors.Is(err, context.DeadlineExceeded) || err == nil {
				t.Fatalf("shutdown left admission socket open: %v", err)
			}
			if abort {
				receiveBounded(t, closed)
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

func TestHardeningServerWorkerReservation(t *testing.T) {
	// Source-proven original Add/Wait gap: test the reservation directly, without
	// pretending a scheduler stress test deterministically reproduces the race.
	synctest.Test(t, func(t *testing.T) {
		s, err := NewServer(validServerConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		if !s.registerHandshake(a) {
			t.Fatal("registration rejected")
		}
		defer s.wg.Done()
		s.beginShutdown()
		waited := make(chan struct{})
		go func() { s.wg.Wait(); close(waited) }()
		synctest.Wait()
		select {
		case <-waited:
			t.Fatal("Wait returned before reserved worker completed")
		default:
		}
		s.unregisterHandshake(a)
		if s.registerHandshake(a) {
			t.Fatal("registered after shutdown gate")
		}
	})
}

func TestHardeningHandoffShutdownStress(t *testing.T) {
	for range 20 {
		s, err := NewServer(validServerConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		session, _ := testSessions(t)
		c := NewConnection(NewTCPTransport(a), session, ConnectionConfig{})
		if !s.registerHandshake(a) {
			t.Fatal("registration rejected")
		}
		start := make(chan struct{})
		registered := make(chan bool, 1)
		shutdown := make(chan struct{})
		go func() { <-start; registered <- s.registerConn(c) }()
		go func() { <-start; _ = s.Abort(); close(shutdown) }()
		close(start)
		receiveBounded(t, registered)
		receiveBounded(t, shutdown)
		// Whichever side wins handoff, Abort must own and close the socket.
		ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
		_, err = NewTCPTransport(b).ReadFrame(ctx)
		cancel()
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("handoff lost socket")
		}
		s.unregisterHandshake(a)
		s.unregisterConn(c)
		s.wg.Done()
		_ = c.Close()
		_ = b.Close()
		_ = s.Close()
	}
}

func TestHardeningNegativeHandlerShutdownTimeout(t *testing.T) {
	config := validServerConfig(t)
	config.HandlerShutdownTimeout = -time.Second
	if _, err := NewServer(config); err == nil {
		t.Fatal("negative HandlerShutdownTimeout accepted with valid static key")
	}
}
