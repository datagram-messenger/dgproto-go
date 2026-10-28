//go:build go1.26

package dgproto

import (
	"net"
	"testing"
	"testing/synctest"
)

func TestServerStatsAtomicHandoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, err := NewServer(validServerConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		session, peer := testSessions(t)
		defer session.Close()
		defer peer.Close()
		conn := NewConnection(NewTCPTransport(a), session, ConnectionConfig{})
		if !s.registerHandshake(a) {
			t.Fatal("registration rejected")
		}
		defer s.wg.Done()
		defer s.unregisterHandshake(a)
		defer s.unregisterConn(conn)
		if got := s.Stats(); got.PendingConnections != 1 || got.ActiveConnections != 0 {
			t.Fatalf("before handoff: %+v", got)
		}
		start := make(chan struct{})
		sampled := make(chan struct{})
		go func() {
			<-start
			for range 1000 {
				got := s.Stats()
				if got.PendingConnections+got.ActiveConnections != 1 {
					t.Errorf("incoherent ownership: %+v", got)
				}
			}
			close(sampled)
		}()
		handed := make(chan bool, 1)
		go func() { <-start; handed <- s.registerConn(conn) }()
		close(start)
		if !receiveBounded(t, handed) {
			t.Fatal("handoff rejected")
		}
		receiveBounded(t, sampled)
		// No Start call: active is ownership, not live-ready runtime state.
		if got := s.Stats(); got.PendingConnections != 0 || got.ActiveConnections != 1 {
			t.Fatalf("after handoff: %+v", got)
		}
		s.beginShutdown()
		if got := s.Stats(); !got.ShuttingDown || got.PendingConnections != 0 || got.ActiveConnections != 1 {
			t.Fatalf("shutdown snapshot: %+v", got)
		}
	})
}
