package dgproto_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func startedClientFixture(t *testing.T, handler dgproto.MessageHandler) (string, dgproto.ClientConfig) {
	t.Helper()
	client, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	server, err := dgproto.NewServer(dgproto.ServerConfig{
		StaticKey: key, AllowedClients: [][]byte{client.Public()},
		HandshakeTimeout: time.Second, WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second,
		Handler: handler,
	})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Abort()
		_ = server.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not exit")
		}
	})
	return listener.Addr().String(), dgproto.ClientConfig{StaticKey: client, ServerStatic: key.Public(), ConnectionConfig: dgproto.ConnectionConfig{HandshakeTimeout: time.Second, WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second}}
}

func awaitStartedClient(t *testing.T, c *dgproto.Connection) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not exit")
	}
}

func TestDialAndStartContextLifetimeAndHandler(t *testing.T) {
	address, config := startedClientFixture(t, func(ctx context.Context, c *dgproto.Connection, message any) error {
		if ack, ok := message.(*dgproto.Ack); ok {
			return c.SendAndWait(ctx, *ack)
		}
		return nil
	})
	received := make(chan uint64, 1)
	config.Handler = func(ctx context.Context, c *dgproto.Connection, message any) error {
		if ack, ok := message.(*dgproto.Ack); ok && len(ack.Sequences) == 1 {
			select {
			case received <- ack.Sequences[0]:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	establishment, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	runtime, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, info, err := dgproto.DialAndStartContext(establishment, runtime, address, config)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(); awaitStartedClient(t, c) })
	if info.SessionID == ([16]byte{}) || info.RemoteAddr == nil {
		t.Fatal("missing authenticated metadata")
	}
	writeCtx, stopWrite := context.WithTimeout(runtime, 5*time.Second)
	defer stopWrite()
	if err := c.SendAndWait(writeCtx, dgproto.Ack{Sequences: []uint64{42}}); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-received:
		if n != 42 {
			t.Fatal("wrong echo")
		}
	case <-writeCtx.Done():
		t.Fatal("configured handler did not receive echo")
	}
	cancel()
	awaitStartedClient(t, c)
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatalf("terminal cause: %v", c.Err())
	}
}

func TestDialAndStartContextRejectsContextsAndConfig(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		establishment, runtime context.Context
		want                   error
	}{
		{"nil establishment", nil, context.Background(), dgproto.ErrInvalidClientConfig},
		{"nil runtime", context.Background(), nil, dgproto.ErrInvalidClientConfig},
		{"canceled runtime", context.Background(), canceledStartContext(), context.Canceled},
		{"canceled establishment", canceledStartContext(), context.Background(), dgproto.ErrInvalidStaticKey},
		{"invalid config", context.Background(), context.Background(), dgproto.ErrInvalidStaticKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info, err := dgproto.DialAndStartContext(tc.establishment, tc.runtime, "not an address", dgproto.ClientConfig{})
			if c != nil || info != (dgproto.AdmissionInfo{}) || !errors.Is(err, tc.want) {
				t.Fatalf("unexpected result: connection=%v err=%v", c != nil, err)
			}
		})
	}
}

func canceledStartContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestDialAndStartContextRuntimeCanceledDuringHandshake(t *testing.T) {
	key, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	client, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	establishment, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	runtime, cancel := context.WithCancel(context.Background())
	defer cancel()
	peerDone := make(chan error, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			peerDone <- err
			return
		}
		defer raw.Close()
		transport := dgproto.NewTCPTransport(raw)
		h, err := dgproto.NewResponderHandshake(key, nil)
		if err != nil {
			peerDone <- err
			return
		}
		first, err := transport.ReadFrame(establishment)
		if err != nil {
			peerDone <- err
			return
		}
		if err = h.ReadFlight(first.Payload); err != nil {
			peerDone <- err
			return
		}
		flight, err := h.WriteFlight()
		if err != nil {
			peerDone <- err
			return
		}
		// Cancel only runtime, before the response permits establishment to finish.
		cancel()
		frame := dgproto.Frame{Header: dgproto.NewHeader(dgproto.MessageTypeHandshakeResponse, [16]byte{}, 0, uint32(len(flight)), 0), Payload: flight}
		if err = transport.WriteFrame(establishment, frame); err != nil {
			peerDone <- err
			return
		}
		final, err := transport.ReadFrame(establishment)
		if err != nil {
			peerDone <- err
			return
		}
		if err = h.ReadFlight(final.Payload); err != nil {
			peerDone <- err
			return
		}
		_, err = transport.ReadFrame(establishment)
		if err == nil {
			peerDone <- errors.New("unexpected runtime frame")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			peerDone <- errors.New("socket was not closed")
			return
		}
		peerDone <- nil
	}()
	config := dgproto.ClientConfig{StaticKey: client, ServerStatic: key.Public(), ConnectionConfig: dgproto.ConnectionConfig{Handler: func(context.Context, *dgproto.Connection, any) error { t.Error("runtime handler started"); return nil }}}
	c, info, err := dgproto.DialAndStartContext(establishment, runtime, listener.Addr().String(), config)
	if c != nil || info != (dgproto.AdmissionInfo{}) || !errors.Is(err, context.Canceled) {
		t.Fatalf("connection=%v err=%v", c != nil, err)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-establishment.Done():
		t.Fatal("peer cleanup stuck")
	}
}

func TestDialAndStartContextWrongPinAndHandlerFailure(t *testing.T) {
	address, config := startedClientFixture(t, func(ctx context.Context, c *dgproto.Connection, message any) error {
		return c.SendAndWait(ctx, dgproto.Ack{Sequences: []uint64{1}})
	})
	wrong, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	bad := config
	bad.ServerStatic = wrong.Public()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, info, err := dgproto.DialAndStartContext(ctx, ctx, address, bad)
	if c != nil || info != (dgproto.AdmissionInfo{}) || !errors.Is(err, dgproto.ErrHandshakeFailed) {
		t.Fatalf("wrong-pin result: %v", err)
	}
	cause := errors.New("application handler rejected message")
	config.Handler = func(context.Context, *dgproto.Connection, any) error { return cause }
	c, _, err = dgproto.DialAndStartContext(ctx, ctx, address, config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SendAndWait(ctx, dgproto.Ack{Sequences: []uint64{7}}); err != nil {
		t.Fatal(err)
	}
	awaitStartedClient(t, c)
	if !errors.Is(c.Err(), cause) {
		t.Fatalf("handler cause: %v", c.Err())
	}
}
