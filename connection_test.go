package dgproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

const connectionTestTimeout = time.Second

func testConnectionPair(t *testing.T, config ConnectionConfig) (*Connection, *Session, *TCPTransport, func()) {
	t.Helper()
	localConn, peerConn := net.Pipe()
	localSession, peerSession := testSessions(t)
	connection := NewConnection(NewTCPTransport(localConn), localSession, config)
	peerTransport := NewTCPTransport(peerConn)
	cleanup := func() {
		_ = peerTransport.Close()
		_ = connection.Close()
		select {
		case <-connection.Done():
		case <-time.After(connectionTestTimeout):
			t.Fatal("connection did not stop")
		}
	}
	return connection, peerSession, peerTransport, cleanup
}

func writeConnectionMessage(t *testing.T, transport *TCPTransport, session *Session, message any) {
	t.Helper()
	for {
		frame, err := session.Send(message, 0)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
		err = transport.WriteFrame(ctx, frame)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if frame.Header.MessageType != MessageTypeRekeyInit {
			return
		}
		if err := session.MarkRekeySent(frame); err != nil {
			t.Fatal(err)
		}
	}
}

func readConnectionMessage(t *testing.T, transport *TCPTransport, session *Session) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	frame, err := transport.ReadFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	message, err := session.Receive(frame)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func waitConnection(t *testing.T, connection *Connection) {
	t.Helper()
	select {
	case <-connection.Done():
	case <-time.After(connectionTestTimeout):
		t.Fatal("connection did not stop")
	}
}

func TestNewConnectionKeepaliveTimeoutDefault(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{name: "normal", interval: time.Second, want: 2 * time.Second},
		{name: "boundary", interval: time.Duration(math.MaxInt64 / 2), want: time.Duration(math.MaxInt64 - 1)},
		{name: "overflow saturates", interval: time.Duration(math.MaxInt64/2 + 1), want: time.Duration(math.MaxInt64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			localConn, peerConn := net.Pipe()
			defer peerConn.Close()
			localSession, _ := testSessions(t)
			connection := NewConnection(NewTCPTransport(localConn), localSession, ConnectionConfig{
				KeepaliveInterval: test.interval,
			})
			defer connection.Close()

			if connection.config.KeepaliveTimeout != test.want {
				t.Fatalf("KeepaliveTimeout = %v, want %v", connection.config.KeepaliveTimeout, test.want)
			}
			if connection.config.KeepaliveTimeout <= 0 {
				t.Fatalf("KeepaliveTimeout = %v, want a nonpositive/immediate timeout to be impossible", connection.config.KeepaliveTimeout)
			}
		})
	}
}

func TestConnectionEncryptedMessageDispatch(t *testing.T) {
	received := make(chan *EncryptedData, 1)
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		Handler: func(_ context.Context, _ *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				t.Fatalf("message type = %T", message)
			}
			received <- data
			return nil
		},
	})
	defer cleanup()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 7, AppMessageType: 3})
	select {
	case got := <-received:
		if got.StreamID != 7 || got.AppMessageType != 3 {
			t.Fatalf("message = %#v", got)
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("handler was not called")
	}
}

func TestConnectionPingPong(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, PingPong{Nonce: 42})
	got, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
	if !ok || !got.IsResponse || got.Nonce != 42 {
		t.Fatalf("response = %#v", got)
	}
}

func TestConnectionRekeyContinues(t *testing.T) {
	received := make(chan *EncryptedData, 2)
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		Handler: func(_ context.Context, _ *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				return nil
			}
			received <- data
			return nil
		},
	})
	defer cleanup()
	connection.Start(context.Background())

	peerSession.sendMu.Lock()
	peerSession.rekeyFrameLimit = 1
	peerSession.sendMu.Unlock()
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 1, AppMessageType: 11})
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 2, AppMessageType: 22})

	for i, want := range []EncryptedData{
		{StreamID: 1, AppMessageType: 11},
		{StreamID: 2, AppMessageType: 22},
	} {
		select {
		case got := <-received:
			if got.StreamID != want.StreamID || got.AppMessageType != want.AppMessageType {
				t.Fatalf("message[%d] = %#v, want %#v", i, got, want)
			}
		case <-time.After(connectionTestTimeout):
			t.Fatal("connection stopped dispatching after rekey")
		}
	}
	select {
	case <-connection.Done():
		t.Fatalf("connection stopped after valid rekey: %v", connection.Err())
	default:
	}
}

func TestConnectionRemoteSessionClose(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, SessionClose{Code: 1, Reason: "done"})
	waitConnection(t, connection)
	if !errors.Is(connection.Err(), ErrConnectionClosed) {
		t.Fatalf("error = %v", connection.Err())
	}
}

func TestConnectionContextCancellation(t *testing.T) {
	connection, _, _, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	connection.Start(ctx)
	cancel()
	waitConnection(t, connection)
	if !errors.Is(connection.Err(), context.Canceled) {
		t.Fatalf("error = %v", connection.Err())
	}
}

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

func TestConnectionHandlerFailureContainment(t *testing.T) {
	handlerErr := errors.New("handler failed")
	for _, test := range []struct {
		name    string
		handler MessageHandler
		want    error
	}{
		{
			name: "error",
			handler: func(context.Context, *Connection, any) error {
				return handlerErr
			},
			want: handlerErr,
		},
		{
			name: "panic",
			handler: func(context.Context, *Connection, any) error {
				panic("boom")
			},
			want: ErrHandlerPanic,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{Handler: test.handler})
			defer cleanup()
			connection.Start(context.Background())
			writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
			waitConnection(t, connection)
			if !errors.Is(connection.Err(), test.want) {
				t.Fatalf("error = %v, want %v", connection.Err(), test.want)
			}
		})
	}
}

func TestConnectionIdleTimeoutSendsSessionClose(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		IdleTimeout:  20 * time.Millisecond,
		WriteTimeout: connectionTestTimeout,
	})
	defer cleanup()
	connection.Start(context.Background())

	got, ok := readConnectionMessage(t, peerTransport, peerSession).(*SessionClose)
	if !ok || got.Code != 3 {
		t.Fatalf("close = %#v", got)
	}
	waitConnection(t, connection)
	if !errors.Is(connection.Err(), ErrIdleTimeout) {
		t.Fatalf("error = %v", connection.Err())
	}
}

func TestConnectionPeriodicKeepaliveAndPong(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		KeepaliveInterval: 20 * time.Millisecond,
	})
	defer cleanup()
	connection.Start(context.Background())

	var previous uint64
	for cycle := range 3 {
		ping, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
		if !ok || ping.IsResponse || ping.Nonce <= previous {
			t.Fatalf("cycle %d ping = %#v, previous nonce %d", cycle, ping, previous)
		}
		previous = ping.Nonce
		writeConnectionMessage(t, peerTransport, peerSession, PingPong{IsResponse: true, Nonce: ping.Nonce})
	}
	select {
	case <-connection.Done():
		t.Fatalf("connection stopped after valid pong cycles: %v", connection.Err())
	default:
	}
}

func TestConnectionKeepalivePongMatchingWrongStaleAndMissing(t *testing.T) {
	for _, test := range []struct {
		name        string
		pong        func(*testing.T, *TCPTransport, *Session, uint64)
		wantTimeout bool
	}{
		{name: "matching", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce})
		}},
		{name: "wrong", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce + 1})
		}, wantTimeout: true},
		{name: "stale", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce - 1})
		}, wantTimeout: true},
		{name: "missing", wantTimeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
				KeepaliveInterval: 10 * time.Millisecond,
				KeepaliveTimeout:  100 * time.Millisecond,
			})
			defer cleanup()
			connection.Start(context.Background())
			ping, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
			if !ok || ping.IsResponse || ping.Nonce == 0 {
				t.Fatalf("ping = %#v", ping)
			}
			if test.pong != nil {
				test.pong(t, peerTransport, peerSession, ping.Nonce)
			}
			if test.wantTimeout {
				waitConnection(t, connection)
				if !errors.Is(connection.Err(), ErrKeepaliveTimeout) {
					t.Fatalf("error = %v", connection.Err())
				}
				return
			}
			select {
			case <-connection.Done():
				t.Fatalf("connection stopped after matching pong: %v", connection.Err())
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestConnectionSlowHandlerPreservesOrderAndBoundsQueue(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHandler()

	started := make(chan struct{}, 1)
	processed := make(chan uint16, 3)
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		HandlerQueue: 2,
		Handler: func(_ context.Context, _ *Connection, message any) error {
			data := message.(*EncryptedData)
			if data.StreamID == 1 {
				started <- struct{}{}
				<-release
			}
			processed <- data.StreamID
			return nil
		},
	})
	defer cleanup()
	connection.Start(context.Background())
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 1})
	select {
	case <-started:
	case <-time.After(connectionTestTimeout):
		releaseHandler()
		t.Fatal("handler did not start")
	}
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 2})
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{StreamID: 3})

	writer := make(chan error, 1)
	go func() {
		frame, err := peerSession.Send(EncryptedData{StreamID: 4}, 0)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			err = peerTransport.WriteFrame(ctx, frame)
			cancel()
		}
		writer <- err
	}()

	var backpressureErr error
	select {
	case <-connection.ctx.Done():
		backpressureErr = connection.Err()
	case <-time.After(connectionTestTimeout):
		backpressureErr = errors.New("handler queue did not apply backpressure")
	}
	releaseHandler()

	select {
	case err := <-writer:
		if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("fourth write: %v", err)
		}
	case <-time.After(connectionTestTimeout):
		t.Error("fourth write did not finish")
	}
	waitConnection(t, connection)
	if backpressureErr != nil && !errors.Is(backpressureErr, ErrHandlerQueueFull) {
		t.Error(backpressureErr)
	}
	if !errors.Is(connection.Err(), ErrHandlerQueueFull) {
		t.Fatalf("error = %v, want %v", connection.Err(), ErrHandlerQueueFull)
	}
	select {
	case got := <-processed:
		if got != 1 {
			t.Fatalf("processed = %d, want only in-flight callback 1", got)
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("in-flight callback did not finish")
	}
	select {
	case got := <-processed:
		t.Fatalf("queued callback %d started after cancellation", got)
	default:
	}
}

func TestConnectionShutdownCancelsBusyHandler(t *testing.T) {
	started := make(chan struct{})
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		Handler: func(ctx context.Context, _ *Connection, _ any) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer cleanup()
	connection.Start(context.Background())
	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
	select {
	case <-started:
	case <-time.After(connectionTestTimeout):
		t.Fatal("handler did not start")
	}
	_ = peerTransport.Close()
	waitConnection(t, connection)
}

func TestConnectionLocalCloseSendsSessionClose(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		WriteTimeout: connectionTestTimeout,
	})
	defer cleanup()
	connection.Start(context.Background())

	closed := make(chan error, 1)
	go func() { closed <- connection.Close() }()
	got, ok := readConnectionMessage(t, peerTransport, peerSession).(*SessionClose)
	if !ok || got.Code != 0 {
		t.Fatalf("close = %#v", got)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	waitConnection(t, connection)
}

func TestConnectionClosePrefersBufferedWriteResultOverCancellation(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })

	writeErr := errors.New("close write failed")
	for iteration := range 100 {
		localConn, peerConn := net.Pipe()
		localSession, _ := testSessions(t)
		connection := NewConnection(NewTCPTransport(localConn), localSession, ConnectionConfig{
			WriteTimeout: connectionTestTimeout,
		})
		connection.started.Store(true)

		closed := make(chan error, 1)
		go func() {
			closed <- connection.closeGracefully(SessionClose{}, ErrConnectionClosed)
		}()

		request := <-connection.closeRequest
		// Let closeGracefully block waiting for a result. Cancellation commits
		// the blocked select first; the buffered result must still take priority.
		runtime.Gosched()
		connection.cancel(ErrConnectionClosed)
		request.result <- writeErr
		runtime.Gosched()

		if err := <-closed; !errors.Is(err, writeErr) {
			t.Fatalf("iteration %d: Close error = %v, want buffered write result %v", iteration, err, writeErr)
		}
		_ = peerConn.Close()
	}
}

func TestConnectionCloseIdempotent(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{WriteTimeout: connectionTestTimeout})
	defer cleanup()
	connection.Start(context.Background())

	closed := make(chan error, 1)
	go func() { closed <- connection.Close() }()
	if _, ok := readConnectionMessage(t, peerTransport, peerSession).(*SessionClose); !ok {
		t.Fatal("expected SessionClose")
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	waitConnection(t, connection)
	if !errors.Is(connection.Err(), ErrConnectionClosed) {
		t.Fatalf("error = %v", connection.Err())
	}
	if err := connection.Send(PingPong{}); !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("send error = %v", err)
	}
}

func TestTerminalCauseRemainsFirstObservedCause(t *testing.T) {
	first := errors.New("first terminal cause")
	later := []error{
		io.EOF,
		context.Canceled,
		errors.New("later transport failure"),
		fmt.Errorf("%w: later panic", ErrHandlerPanic),
	}

	connection := &Connection{}
	connection.recordTerminal(first)
	for _, cause := range later {
		connection.recordTerminal(cause)
		if !errors.Is(connection.Err(), first) {
			t.Fatalf("terminal cause changed to %v after recording %v", connection.Err(), cause)
		}
	}
}
