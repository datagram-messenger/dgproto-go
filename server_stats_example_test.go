package dgproto_test

import (
	"fmt"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func ExampleServer_Stats() {
	// Ephemeral demonstration identity; services should provision protected keys.
	key, err := dgproto.GenerateStaticKey()
	if err != nil {
		panic(err)
	}
	server, err := dgproto.NewServer(dgproto.ServerConfig{
		StaticKey: key, MaxConcurrentHandshakes: 2, MaxActiveConnections: 8,
	})
	if err != nil {
		panic(err)
	}
	stats := server.Stats()
	fmt.Printf("handshake slots: %d/%d; connection slots: %d/%d\n",
		stats.HandshakeSlotsUsed, stats.HandshakeSlotsCapacity,
		stats.ConnectionSlotsUsed, stats.ConnectionSlotsCapacity)
	// Gauges are advisory, not capacity reservations or peer-readiness checks.
	if err := server.Close(); err != nil {
		panic(err)
	}
	stats = server.Stats()
	fmt.Printf("pending: %d; active: %d; shutting down: %t\n",
		stats.PendingConnections, stats.ActiveConnections, stats.ShuttingDown)
	// Output:
	// handshake slots: 0/2; connection slots: 0/8
	// pending: 0; active: 0; shutting down: true
}
