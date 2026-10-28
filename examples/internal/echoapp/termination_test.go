package echoapp

import (
	"context"
	"errors"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestDeadlineAfterRequestWithoutReply(t *testing.T) {
	server, client := testKey(t), testKey(t)
	seen := make(chan struct{}, 1)
	disconnected := make(chan struct{}, 1)
	config := testConfig(server, client, func(_ context.Context, _ *dgproto.Connection, message any) error {
		if data, ok := message.(*dgproto.EncryptedData); ok {
			if err := Validate(data, 1); err != nil {
				return err
			}
			seen <- struct{}{}
		}
		return nil // No reply: local write and server receipt are not success.
	})
	config.OnDisconnect = func(context.Context, *dgproto.Connection, dgproto.AdmissionInfo, error) { disconnected <- struct{}{} }
	address := startServer(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Exchange(ctx, address, client, server.Public()) }()
	select {
	case <-seen:
		if ctx.Err() != nil {
			t.Fatal("deadline expired before request observation")
		}
	case <-ctx.Done():
		t.Fatal("request not observed before deadline")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired client did not join")
	}
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("expired client retained server connection")
	}
}

func TestPeerTerminatesAfterRequestBeforeReply(t *testing.T) {
	server, client := testKey(t), testKey(t)
	seen := make(chan struct{}, 1)
	terminated := errors.New("synthetic early peer termination")
	address := startServer(t, testConfig(server, client, func(_ context.Context, _ *dgproto.Connection, message any) error {
		if data, ok := message.(*dgproto.EncryptedData); ok {
			if err := Validate(data, 1); err != nil {
				return err
			}
			seen <- struct{}{}
			return terminated // Runtime closes the peer without an application reply.
		}
		return nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := Exchange(ctx, address, client, server.Public()); err == nil {
		t.Fatal("peer termination became application success")
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Fatalf("peer termination waited for context: %v", err)
	}
	select {
	case <-seen:
	default:
		t.Fatal("peer terminated before validated request")
	}
}
