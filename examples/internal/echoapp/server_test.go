package echoapp

import (
	"context"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestProvisionedServerEvictsIdlePeer(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the real 15-second example idle budget")
	}
	serverKey, clientKey := testKey(t), testKey(t)
	server, err := NewServer(serverKey, clientKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = server.Close()
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
	establishCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	client, _, err := dgproto.DialContext(establishCtx, listener.Addr().String(), dgproto.ClientConfig{
		StaticKey: clientKey, ServerStatic: serverKey.Public(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.Start(ctx)
	// No inbound application/control traffic: wait on runtime completion rather
	// than guessing admission timing or polling counters. This timeout is only
	// a test watchdog; the server's configured idle budget must cause closure.
	select {
	case <-client.Done():
		if client.Err() == nil {
			t.Fatal("idle peer closed without a terminal cause")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("idle peer retained an active slot beyond the example budget")
	}
}
