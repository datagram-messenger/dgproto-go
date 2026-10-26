package dgproto

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

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
		// Model Start's ownership without launching the real transport loops.
		connection.startOnce.Do(func() { connection.started.Store(true) })

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

func TestConnectionHandlerShutdownTimeoutUnblocksClose(t *testing.T) {
	// Regression test for DGP-031-05: Close() must not hang indefinitely when
	// a MessageHandler ignores its context. With HandlerShutdownTimeout set,
	// the connection waitgroup must complete within the deadline even if the
	// handler goroutine is still running.
	handlerStarted := make(chan struct{})
	handlerReleased := make(chan struct{})
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		HandlerShutdownTimeout: 50 * time.Millisecond,
		Handler: func(_ context.Context, _ *Connection, _ any) error {
			close(handlerStarted)
			<-handlerReleased // deliberately ignores ctx — simulates a stuck handler
			return nil
		},
	})
	defer func() {
		close(handlerReleased) // unblock the detached goroutine so it can exit
		cleanup()
	}()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
	select {
	case <-handlerStarted:
	case <-time.After(connectionTestTimeout):
		t.Fatal("handler did not start")
	}

	// Close the peer transport to trigger connection shutdown while the handler
	// is still blocked. Done() must close within the shutdown timeout + margin.
	_ = peerTransport.Close()
	select {
	case <-connection.Done():
	case <-time.After(connectionTestTimeout):
		t.Fatal("connection did not stop: Close() blocked on stuck handler despite HandlerShutdownTimeout")
	}
}

func TestConnectionHandlerShutdownTimeoutZeroPreservesBlockingBehavior(t *testing.T) {
	// With HandlerShutdownTimeout == 0 (disabled), the connection must wait for
	// the handler to return — the existing behavior is preserved.
	handlerStarted := make(chan struct{})
	handlerReleased := make(chan struct{})
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		HandlerShutdownTimeout: 0, // disabled
		Handler: func(ctx context.Context, _ *Connection, _ any) error {
			close(handlerStarted)
			<-ctx.Done() // well-behaved: respects context
			<-handlerReleased
			return ctx.Err()
		},
	})
	defer cleanup()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, EncryptedData{})
	select {
	case <-handlerStarted:
	case <-time.After(connectionTestTimeout):
		t.Fatal("handler did not start")
	}

	_ = peerTransport.Close()
	// Handler is still blocking on handlerReleased; Done() must not fire yet.
	select {
	case <-connection.Done():
		t.Fatal("connection stopped before handler was released")
	case <-time.After(20 * time.Millisecond):
	}
	close(handlerReleased)
	select {
	case <-connection.Done():
	case <-time.After(connectionTestTimeout):
		t.Fatal("connection did not stop after handler returned")
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
