package echoapp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func testKey(t *testing.T) dgproto.StaticKey {
	t.Helper()
	key, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func startServer(t *testing.T, config dgproto.ServerConfig) string {
	t.Helper()
	server, err := dgproto.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, server, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not join")
		}
	})
	return listener.Addr().String()
}

func testConfig(server, client dgproto.StaticKey, handler dgproto.MessageHandler) dgproto.ServerConfig {
	return dgproto.ServerConfig{StaticKey: server, AllowedClients: [][]byte{client.Public()},
		HandshakeTimeout: time.Second, WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second, Handler: handler}
}

func TestIndependentExchange(t *testing.T) {
	server, client := testKey(t), testKey(t)
	address := startServer(t, testConfig(server, client, Reply))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, client, server.Public()); err != nil {
		t.Fatal(err)
	}
	// A second independent consumer uses the same provisioned identities.
	if err := Exchange(ctx, address, client, server.Public()); err != nil {
		t.Fatal(err)
	}
}

func TestWrongPin(t *testing.T) {
	server, client, other := testKey(t), testKey(t), testKey(t)
	address := startServer(t, testConfig(server, client, Reply))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, client, other.Public()); !errors.Is(err, dgproto.ErrHandshakeFailed) {
		t.Fatalf("error = %v", err)
	}
}

func TestCanceledWhileWaitingForReply(t *testing.T) {
	server, client := testKey(t), testKey(t)
	seen := make(chan struct{}, 1)
	address := startServer(t, testConfig(server, client, func(ctx context.Context, _ *dgproto.Connection, message any) error {
		if _, ok := message.(*dgproto.EncryptedData); ok {
			seen <- struct{}{}
		}
		return nil // Deliberately no application reply.
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Exchange(ctx, address, client, server.Public()) }()
	select {
	case <-seen:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("request not received")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled client did not join")
	}
}

func TestMalformedApplicationReply(t *testing.T) {
	server, client := testKey(t), testKey(t)
	address := startServer(t, testConfig(server, client, func(ctx context.Context, c *dgproto.Connection, message any) error {
		if _, ok := message.(*dgproto.EncryptedData); !ok {
			return nil
		}
		return c.SendAndWait(ctx, dgproto.EncryptedData{StreamID: 99, AppMessageType: 2,
			Fields: []dgproto.TLV{{Type: 1, Value: []byte(Payload)}}})
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, client, server.Public()); !errors.Is(err, ErrEnvelope) {
		t.Fatalf("error = %v", err)
	}
}

func TestServePreCanceledOwnsListener(t *testing.T) {
	server, err := NewServer(testKey(t), testKey(t).Public())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, server, listener); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("listener not closed: %v", err)
	}
}

func TestExplicitAllowlistRequired(t *testing.T) {
	for _, pin := range [][]byte{nil, make([]byte, 32), {1}} {
		if server, err := NewServer(testKey(t), pin); server != nil || !errors.Is(err, ErrIdentityInput) {
			t.Fatal("invalid allowlist accepted")
		}
	}
}
