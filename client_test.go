package dgproto

import (
	"context"
	"errors"
	"math"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func clientTestConfig(t *testing.T) (ClientConfig, StaticKey) {
	t.Helper()
	client, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	server, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	return ClientConfig{StaticKey: client, ServerStatic: server.Public()}, server
}

func TestDialContextRejectsConfigBeforeDial(t *testing.T) {
	good, _ := clientTestConfig(t)
	for _, tc := range []struct {
		name   string
		change func(*ClientConfig)
		want   error
	}{
		{"missing identity", func(c *ClientConfig) { c.StaticKey = StaticKey{} }, ErrInvalidStaticKey},
		{"missing pin", func(c *ClientConfig) { c.ServerStatic = nil }, ErrInvalidClientConfig},
		{"short pin", func(c *ClientConfig) { c.ServerStatic = []byte{1} }, ErrInvalidClientConfig},
		{"zero pin", func(c *ClientConfig) { c.ServerStatic = make([]byte, 32) }, ErrInvalidClientConfig},
		{"negative timeout", func(c *ClientConfig) { c.HandshakeTimeout = -1 }, ErrInvalidClientConfig},
		{"keepalive default overflow", func(c *ClientConfig) { c.KeepaliveInterval = time.Duration(math.MaxInt64) }, ErrInvalidClientConfig},
		{"keepalive window overflow", func(c *ClientConfig) { c.KeepaliveInterval = time.Duration(math.MaxInt64); c.KeepaliveTimeout = 1 }, ErrInvalidClientConfig},
		{"long pin", func(c *ClientConfig) { c.ServerStatic = make([]byte, 33) }, ErrInvalidClientConfig},
		{"negative queue", func(c *ClientConfig) { c.OutboundQueue = -1 }, ErrInvalidClientConfig},
		{"negative shutdown", func(c *ClientConfig) { c.HandlerShutdownTimeout = -1 }, ErrInvalidClientConfig},
		{"liveness", func(c *ClientConfig) { c.ReadTimeout = time.Second; c.IdleTimeout = 2 * time.Second }, ErrInvalidClientConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := good
			tc.change(&c)
			called := false
			conn, info, err := dialContext(context.Background(), "unused", c, func(context.Context, string, string) (net.Conn, error) {
				called = true
				return nil, errors.New("unexpected dial")
			})
			if !errors.Is(err, tc.want) || called || conn != nil || info != (AdmissionInfo{}) {
				t.Fatalf("conn=%v info=%v err=%v dial=%v", conn, info, err, called)
			}
		})
	}
	for _, ctx := range []context.Context{nil, canceledClientContext()} {
		called := false
		_, _, err := dialContext(ctx, "unused", good, func(context.Context, string, string) (net.Conn, error) { called = true; return nil, nil })
		if err == nil || called {
			t.Fatalf("err=%v called=%v", err, called)
		}
	}
}
func canceledClientContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

type clientCountConn struct {
	net.Conn
	closes atomic.Int32
}

func (c *clientCountConn) Close() error { c.closes.Add(1); return c.Conn.Close() }

func TestDialContextHandshakeCancellationClosesOnce(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			config, _ := clientTestConfig(t)
			local, peer := net.Pipe()
			defer peer.Close()
			counted := &clientCountConn{Conn: local}
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			if deadline {
				config.HandshakeTimeout = 50 * time.Millisecond
			}
			entered := make(chan struct{})
			peerDone := make(chan error, 1)
			go func() {
				_, err := NewTCPTransport(peer).ReadFrame(ctx)
				close(entered)
				if err != nil {
					peerDone <- err
					return
				}
				var b [1]byte
				_, err = peer.Read(b[:])
				peerDone <- err
			}()
			result := make(chan error, 1)
			go func() {
				c, info, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) { return counted, nil })
				if c != nil || info != (AdmissionInfo{}) {
					result <- errors.New("failure leaked result")
					return
				}
				result <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("flight 1 did not arrive")
			}
			if !deadline {
				cancel()
			}
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(connectionTestTimeout):
				t.Fatal("dial stuck")
			}
			if counted.closes.Load() != 1 {
				t.Fatalf("closes=%d", counted.closes.Load())
			}
			select {
			case err := <-peerDone:
				if err == nil {
					t.Fatal("peer not closed")
				}
			case <-time.After(connectionTestTimeout):
				t.Fatal("peer stuck")
			}
		})
	}
}

func TestDialContextCanceledDuringDial(t *testing.T) {
	config, _ := clientTestConfig(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, _, err := dialContext(ctx, "unused", config, func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			close(entered)
			<-dialCtx.Done()
			return nil, dialCtx.Err()
		})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(connectionTestTimeout):
		cancel()
		t.Fatal("dial not entered")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("dial not canceled")
	}
}

func TestDialContextDialDeadlinePrecedence(t *testing.T) {
	config, _ := clientTestConfig(t)
	config.HandshakeTimeout = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := dialContext(ctx, "unused", config, func(dialCtx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Errorf("network=%s", network)
		}
		if d, _ := dialCtx.Deadline(); d.After(time.Now().Add(time.Second)) {
			t.Error("caller deadline not applied")
		}
		<-dialCtx.Done()
		return nil, dialCtx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	config.HandshakeTimeout = 0
	config, err = normalizeClientConfig(config)
	if err != nil || config.HandshakeTimeout != 10*time.Second {
		t.Fatalf("default=%v err=%v", config.HandshakeTimeout, err)
	}
}
