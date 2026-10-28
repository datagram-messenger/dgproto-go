package echoapp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestUnauthorizedClientNeverDispatches(t *testing.T) {
	server, permitted, rejected := testKey(t), testKey(t), testKey(t)
	dispatched := make(chan struct{}, 1)
	address := startServer(t, testConfig(server, permitted, func(context.Context, *dgproto.Connection, any) error {
		dispatched <- struct{}{}
		return nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, rejected, server.Public()); err == nil {
		t.Fatal("unauthorized echo succeeded")
	}
	select {
	case <-dispatched:
		t.Fatal("unauthorized application dispatched")
	default:
	}
}

func TestAdmissionRejectionIsNotDialSuccess(t *testing.T) {
	server, client := testKey(t), testKey(t)
	admitted := make(chan struct{}, 1)
	config := testConfig(server, client, Reply)
	config.Admission = func(context.Context, *dgproto.Connection, dgproto.AdmissionInfo) error {
		admitted <- struct{}{}
		return errors.New("test-only application rejection")
	}
	address := startServer(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, client, server.Public()); err == nil {
		t.Fatal("rejected application succeeded")
	}
	select {
	case <-admitted:
	default:
		t.Fatal("admission was not reached")
	}
}

func TestCancellationDuringStalledHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := listener.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	key, pin := testKey(t), testKey(t).Public()
	go func() { done <- Exchange(ctx, listener.Addr().String(), key, pin) }()
	var peer net.Conn
	select {
	case peer = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("not accepted")
	}
	defer peer.Close()
	cancel() // Peer deliberately never supplies flight 2.
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stalled handshake did not cancel")
	}
	if err := peer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	for {
		_, err := peer.Read(buffer)
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Timeout() {
				t.Fatal("client socket still open")
			}
			break
		}
	}
}

func TestDeadlineBeforeExchange(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := Exchange(ctx, "invalid address", dgproto.StaticKey{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestReplyRejectsInvalidBeforeSend(t *testing.T) {
	if err := Reply(context.Background(), nil, (*dgproto.EncryptedData)(nil)); !errors.Is(err, ErrEnvelope) {
		t.Fatal("typed nil accepted")
	}
	if err := Reply(context.Background(), nil, &dgproto.EncryptedData{}); !errors.Is(err, ErrEnvelope) {
		t.Fatal("malformed request accepted")
	}
	if err := Reply(context.Background(), nil, &dgproto.PingPong{}); err != nil {
		t.Fatal("control message treated as application request")
	}
}
