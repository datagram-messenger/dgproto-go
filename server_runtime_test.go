package dgproto

import (
	"net"
	"testing"
)

// Cover both serialized outcomes of the handoff/shutdown lock, in addition to
// the concurrent socket-ownership stress test in hardening_test.go.
func TestServerHandoffShutdownSnapshots(t *testing.T) {
	for _, handoffFirst := range []bool{false, true} {
		name := "shutdown-first"
		if handoffFirst {
			name = "handoff-first"
		}
		t.Run(name, func(t *testing.T) {
			s, err := NewServer(validServerConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			session, _ := testSessions(t)
			defer session.Close()
			conn := NewConnection(NewTCPTransport(a), session, ConnectionConfig{})
			if !s.registerHandshake(a) {
				t.Fatal("handshake registration rejected")
			}
			defer s.wg.Done()
			defer s.unregisterHandshake(a)
			defer s.unregisterConn(conn)

			if handoffFirst && !s.registerConn(conn) {
				t.Fatal("handoff rejected before shutdown")
			}
			for range 2 {
				listener, conns, sockets := s.beginShutdown()
				if listener != nil {
					t.Fatal("unexpected listener")
				}
				if handoffFirst {
					if len(conns) != 1 || conns[0] != conn || len(sockets) != 0 {
						t.Fatal("shutdown did not exclusively snapshot the runtime")
					}
				} else if len(conns) != 0 || len(sockets) != 1 || sockets[0] != a {
					t.Fatal("shutdown lost the pre-handoff socket")
				}
				if s.registerConn(conn) {
					t.Fatal("handoff accepted after shutdown")
				}
			}
			select {
			case <-s.closed:
			default:
				t.Fatal("shutdown did not close its notification")
			}
		})
	}
}
