package dgproto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A bounded integration probe, not a throughput or deployment certification.
// Every worker owns and joins its runtime; the test joins Serve on all exits.
func TestBoundedTCPConcurrentEcho(t *testing.T) {
	const clients, messages = 16, 16
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverKey, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	var received atomic.Int32
	server, err := NewServer(ServerConfig{
		StaticKey: serverKey, AllowedClients: [][]byte{clientKey.Public()},
		MaxConcurrentHandshakes: clients, MaxActiveConnections: clients,
		HandshakeTimeout: 3 * time.Second, WriteTimeout: 2 * time.Second, HandlerShutdownTimeout: time.Second,
		Handler: func(ctx context.Context, c *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				return errors.New("unexpected message")
			}
			received.Add(1)
			return c.SendPaddedAndWait(ctx, *data, 16)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		cancel()
		_ = server.Abort()
		_ = server.Close()
		select {
		case <-served:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not join")
		}
	}()
	start := make(chan struct{})
	results := make(chan error, clients)
	var workers sync.WaitGroup
	for i := 0; i < clients; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- boundedEchoClient(ctx, listener.Addr().String(), clientKey, serverKey.Public(), messages)
		}()
	}
	close(start)
	joined := make(chan struct{})
	go func() { workers.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-ctx.Done():
		cancel()
		_ = server.Abort()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Fatal("clients did not join after cancellation")
		}
		t.Fatal(ctx.Err())
	}
	for i := 0; i < clients; i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if got := received.Load(); got != clients*messages {
		t.Errorf("received %d, want %d", got, clients*messages)
	}
}

func boundedEchoClient(ctx context.Context, address string, key StaticKey, serverPublic []byte, messages int) (result error) {
	replies := make(chan *EncryptedData, 1)
	c, _, err := DialContext(ctx, address, ClientConfig{StaticKey: key, ServerStatic: serverPublic, ConnectionConfig: ConnectionConfig{
		HandshakeTimeout: 3 * time.Second, WriteTimeout: 2 * time.Second, HandlerShutdownTimeout: time.Second,
		Handler: func(ctx context.Context, _ *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				return errors.New("unexpected echo")
			}
			select {
			case replies <- data:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}})
	if err != nil {
		return err
	}
	defer func() {
		c.abort(ErrConnectionClosed)
		select {
		case <-c.Done():
		case <-time.After(3 * time.Second):
			result = errors.New("client runtime did not join")
		}
	}()
	c.Start(ctx)
	payload := bytes.Repeat([]byte{7}, 4096)
	for i := 0; i < messages; i++ {
		err = c.SendPaddedAndWait(ctx, EncryptedData{StreamID: 1, AppMessageType: 1, Fields: []TLV{{Type: 1, Value: payload}}}, 16)
		if err != nil {
			return err
		}
		select {
		case data := <-replies:
			if data.StreamID != 1 || data.AppMessageType != 1 || len(data.Fields) != 1 || !bytes.Equal(data.Fields[0].Value, payload) {
				return errors.New("echo mismatch")
			}
		case <-c.Done():
			return fmt.Errorf("client terminated: %w", c.Err())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
