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

func TestHardeningRunningHandlerQueueDisposal(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	c, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{HandlerQueue: 4, Handler: func(context.Context, *Connection, any) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}})
	defer cleanup()
	defer close(release)
	c.Start(context.Background())
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
	receiveBounded(t, entered)
	// The response to a later Ping proves readLoop queued the preceding data.
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
	writeConnectionMessage(t, peerTransport, peerSession, PingPong{Nonce: 9})
	if _, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong); !ok {
		t.Fatal("missing read-loop barrier")
	}
	if len(c.handlerQueue) != 1 {
		t.Fatal("expected pending handler reference")
	}
	c.abort(ErrConnectionClosed)
	if len(c.handlerQueue) != 0 {
		t.Fatal("pending handler references retained after abort")
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
