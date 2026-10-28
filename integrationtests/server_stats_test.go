package integrationtests

import (
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestServerStatsConstructed(t *testing.T) {
	for _, limits := range []struct{ handshake, connection int }{{0, 0}, {2, 3}} {
		server, err := dgproto.NewServer(dgproto.ServerConfig{
			StaticKey:               serverTestKey(t, 4),
			MaxConcurrentHandshakes: limits.handshake, MaxActiveConnections: limits.connection,
		})
		if err != nil {
			t.Fatal(err)
		}
		h, c := limits.handshake, limits.connection
		if h == 0 {
			h = 64
		}
		if c == 0 {
			c = 1024
		}
		want := dgproto.ServerStats{HandshakeSlotsCapacity: h, ConnectionSlotsCapacity: c}
		if got := server.Stats(); got != want {
			t.Fatalf("initial Stats = %+v, want %+v", got, want)
		}
		// A returned value does not alias server state.
		copy := server.Stats()
		copy.PendingConnections = 99
		if server.Stats() != want {
			t.Fatal("snapshot mutation changed server")
		}
		if err := server.Close(); err != nil {
			t.Fatal(err)
		}
		want.ShuttingDown = true
		if got := server.Stats(); got != want {
			t.Fatalf("closed Stats = %+v", got)
		}
	}
}
