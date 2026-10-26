package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

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

func TestConnectionRekeyContinues(t *testing.T) {
	received := make(chan *EncryptedData, 2)
	unexpected := make(chan any, 1)
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		Handler: func(_ context.Context, _ *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				unexpected <- message
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
	case message := <-unexpected:
		t.Fatalf("internal message dispatched to handler: %T", message)
	default:
	}
	select {
	case <-connection.Done():
		t.Fatalf("connection stopped after valid rekey: %v", connection.Err())
	default:
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
