//go:build go1.26

package dgproto

import (
	"net"
	"testing"
	"testing/synctest"
)

func TestHardeningServerWorkerReservation(t *testing.T) {
	// Source-proven original Add/Wait gap: test the reservation directly, without
	// pretending a scheduler stress test deterministically reproduces the race.
	synctest.Test(t, func(t *testing.T) {
		s, err := NewServer(validServerConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		if !s.registerHandshake(a) {
			t.Fatal("registration rejected")
		}
		defer s.wg.Done()
		s.beginShutdown()
		waited := make(chan struct{})
		go func() { s.wg.Wait(); close(waited) }()
		synctest.Wait()
		select {
		case <-waited:
			t.Fatal("Wait returned before reserved worker completed")
		default:
		}
		s.unregisterHandshake(a)
		if s.registerHandshake(a) {
			t.Fatal("registered after shutdown gate")
		}
	})
}
