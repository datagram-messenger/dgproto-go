package dgproto

import (
	"context"
	"testing"
	"time"
)

// Withhold maintenance while the authenticated reader processes the entire
// burst. A final Ping's queued response is the processing barrier, not a sleep.
func TestKeepaliveAuthenticatedPongBurst(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nonces []uint64
	}{
		{"stale-matching-stale", []uint64{99, 1, 99}},
		{"matching-stale", []uint64{1, 99}},
		{"stale-matching", []uint64{99, 1}},
		{"flood", append(append(make([]uint64, 256), 1), make([]uint64, 256)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, peer, transport, cleanup := testConnectionPair(t, ConnectionConfig{})
			defer cleanup()
			if c.beginPing() != 1 {
				t.Fatal("unexpected initial nonce")
			}
			exited := make(chan struct{})
			go func() { defer close(exited); c.readLoop() }()
			defer func() {
				c.abort(ErrConnectionClosed)
				select {
				case <-exited:
				case <-time.After(connectionTestTimeout):
					t.Fatal("reader did not join")
				}
			}()
			for _, nonce := range tc.nonces {
				writeConnectionMessage(t, transport, peer, PingPong{IsResponse: true, Nonce: nonce})
			}
			writeConnectionMessage(t, transport, peer, PingPong{Nonce: 123})
			select {
			case <-c.control:
			case <-time.After(connectionTestTimeout):
				t.Fatal("reader barrier not reached")
			}
			select {
			case got := <-c.pong:
				if got != 1 {
					t.Fatalf("matching acknowledgement lost: got %d", got)
				}
			default:
				t.Fatal("matching acknowledgement lost")
			}
		})
	}
}

func TestKeepaliveImmediateReply(t *testing.T) {
	c, peer, transport, cleanup := testConnectionPair(t, ConnectionConfig{KeepaliveInterval: time.Millisecond, KeepaliveTimeout: time.Second})
	defer cleanup()
	c.Start(context.Background())
	for i := 0; i < 10; i++ {
		ping, ok := readConnectionMessage(t, transport, peer).(*PingPong)
		if !ok || ping.IsResponse || ping.Nonce == 0 {
			t.Fatal("expected keepalive Ping")
		}
		writeConnectionMessage(t, transport, peer, PingPong{IsResponse: true, Nonce: ping.Nonce})
		writeConnectionMessage(t, transport, peer, PingPong{IsResponse: true, Nonce: 99})
	}
	if c.Err() != nil {
		t.Fatalf("runtime terminated: %v", c.Err())
	}
}
