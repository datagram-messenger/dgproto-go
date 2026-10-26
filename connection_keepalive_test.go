package dgproto

import (
	"context"
	"errors"
	"math"
	"net"
	"testing"
	"time"
)

func TestNewConnectionKeepaliveTimeoutDefault(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		want     time.Duration
	}{
		{name: "normal", interval: time.Second, want: 2 * time.Second},
		{name: "boundary", interval: time.Duration(math.MaxInt64 / 2), want: time.Duration(math.MaxInt64 - 1)},
		{name: "overflow saturates", interval: time.Duration(math.MaxInt64/2 + 1), want: time.Duration(math.MaxInt64)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			localConn, peerConn := net.Pipe()
			defer peerConn.Close()
			localSession, _ := testSessions(t)
			connection := NewConnection(NewTCPTransport(localConn), localSession, ConnectionConfig{
				KeepaliveInterval: test.interval,
			})
			defer connection.Close()

			if connection.config.KeepaliveTimeout != test.want {
				t.Fatalf("KeepaliveTimeout = %v, want %v", connection.config.KeepaliveTimeout, test.want)
			}
			if connection.config.KeepaliveTimeout <= 0 {
				t.Fatalf("KeepaliveTimeout = %v, want a nonpositive/immediate timeout to be impossible", connection.config.KeepaliveTimeout)
			}
		})
	}
}

func TestConnectionPingPong(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	connection.Start(context.Background())

	writeConnectionMessage(t, peerTransport, peerSession, PingPong{Nonce: 42})
	got, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
	if !ok || !got.IsResponse || got.Nonce != 42 {
		t.Fatalf("response = %#v", got)
	}
}

func TestConnectionIdleTimeoutSendsSessionClose(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		IdleTimeout:  20 * time.Millisecond,
		WriteTimeout: connectionTestTimeout,
	})
	defer cleanup()
	connection.Start(context.Background())

	got, ok := readConnectionMessage(t, peerTransport, peerSession).(*SessionClose)
	if !ok || got.Code != 3 {
		t.Fatalf("close = %#v", got)
	}
	waitConnection(t, connection)
	if !errors.Is(connection.Err(), ErrIdleTimeout) {
		t.Fatalf("error = %v", connection.Err())
	}
}

func TestConnectionPeriodicKeepaliveAndPong(t *testing.T) {
	connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
		KeepaliveInterval: 20 * time.Millisecond,
	})
	defer cleanup()
	connection.Start(context.Background())

	var previous uint64
	for cycle := range 3 {
		ping, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
		if !ok || ping.IsResponse || ping.Nonce <= previous {
			t.Fatalf("cycle %d ping = %#v, previous nonce %d", cycle, ping, previous)
		}
		previous = ping.Nonce
		writeConnectionMessage(t, peerTransport, peerSession, PingPong{IsResponse: true, Nonce: ping.Nonce})
	}
	select {
	case <-connection.Done():
		t.Fatalf("connection stopped after valid pong cycles: %v", connection.Err())
	default:
	}
}

func TestConnectionKeepalivePongMatchingWrongStaleAndMissing(t *testing.T) {
	for _, test := range []struct {
		name        string
		pong        func(*testing.T, *TCPTransport, *Session, uint64)
		wantTimeout bool
	}{
		{name: "matching", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce})
		}},
		{name: "wrong", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce + 1})
		}, wantTimeout: true},
		{name: "stale", pong: func(t *testing.T, tr *TCPTransport, s *Session, nonce uint64) {
			writeConnectionMessage(t, tr, s, PingPong{IsResponse: true, Nonce: nonce - 1})
		}, wantTimeout: true},
		{name: "missing", wantTimeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, peerSession, peerTransport, cleanup := testConnectionPair(t, ConnectionConfig{
				KeepaliveInterval: 10 * time.Millisecond,
				KeepaliveTimeout:  100 * time.Millisecond,
			})
			defer cleanup()
			connection.Start(context.Background())
			ping, ok := readConnectionMessage(t, peerTransport, peerSession).(*PingPong)
			if !ok || ping.IsResponse || ping.Nonce == 0 {
				t.Fatalf("ping = %#v", ping)
			}
			if test.pong != nil {
				test.pong(t, peerTransport, peerSession, ping.Nonce)
			}
			if test.wantTimeout {
				waitConnection(t, connection)
				if !errors.Is(connection.Err(), ErrKeepaliveTimeout) {
					t.Fatalf("error = %v", connection.Err())
				}
				return
			}
			select {
			case <-connection.Done():
				t.Fatalf("connection stopped after matching pong: %v", connection.Err())
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}
