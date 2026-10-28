package dgproto

// ServerStats is a secret-free, advisory snapshot of server resource ownership.
// It contains no keys, identities, payloads, or terminal errors.
type ServerStats struct {
	// HandshakeSlotsUsed and HandshakeSlotsCapacity describe the handshake
	// capacity pool. A slot is released after Noise, before Admission runs.
	HandshakeSlotsUsed     int
	HandshakeSlotsCapacity int

	// ConnectionSlotsUsed and ConnectionSlotsCapacity describe the authenticated
	// connection capacity pool. A slot is acquired before Admission and can
	// remain occupied through OnDisconnect, even after runtime ownership ends.
	ConnectionSlotsUsed     int
	ConnectionSlotsCapacity int

	// PendingConnections counts server-owned sockets before runtime handoff,
	// including sockets whose Noise handshake finished but Admission is pending.
	PendingConnections int

	// ActiveConnections counts handed-off, server-owned connection runtimes.
	// Handoff precedes Start; this is not a count of live or ready peers.
	ActiveConnections int

	// ShuttingDown reports whether Close or Abort has begun shutdown.
	ShuttingDown bool
}

// Stats returns an advisory resource snapshot for a server constructed by
// NewServer. It is safe to call concurrently with Serve, Close, Abort, and from
// callbacks. PendingConnections, ActiveConnections, and ShuttingDown are sampled
// together under the existing ownership mutex, so a handoff cannot double-count
// or lose a socket between the two maps. The channel gauges are independently
// sampled and need not agree with those counts or each other during transitions.
// Stats does not reserve capacity or establish peer readiness. No nil or
// zero-value receiver behavior is guaranteed.
func (s *Server) Stats() ServerStats {
	s.mu.Lock()
	stats := ServerStats{
		PendingConnections: len(s.handshakeConns),
		ActiveConnections:  len(s.conns),
		ShuttingDown:       s.inShutdown.Load(),
	}
	s.mu.Unlock()
	stats.HandshakeSlotsUsed = len(s.handshakes)
	stats.HandshakeSlotsCapacity = cap(s.handshakes)
	stats.ConnectionSlotsUsed = len(s.connSlots)
	stats.ConnectionSlotsCapacity = cap(s.connSlots)
	return stats
}
