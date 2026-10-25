package dgproto

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestServerCloseInterruptsStalledHandshake(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.HandshakeTimeout = 30 * time.Second; return c }())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		tracked := len(server.handshakeConns)
		server.mu.Unlock()
		if tracked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handshake was not tracked")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for HandshakeTimeout")
	}
	select {
	case err := <-serveDone:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestServerCloseIsIdempotent(t *testing.T) {
	server, err := NewServer(validServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
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
