package integrationtests

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

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

func TestServerStatsLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key, peer := serverTestKey(t, 4), serverTestKey(t, 5)
		listener := &lifecycleListener{sockets: make(chan net.Conn), done: make(chan struct{})}
		admissionGate, disconnectGate := make(chan struct{}), make(chan struct{})
		var admissionOnce, disconnectOnce sync.Once
		releaseAdmission := func() { admissionOnce.Do(func() { close(admissionGate) }) }
		releaseDisconnect := func() { disconnectOnce.Do(func() { close(disconnectGate) }) }
		admissionStats, disconnectStats := make(chan dgproto.ServerStats, 1), make(chan dgproto.ServerStats, 1)
		var server *dgproto.Server
		server, err := dgproto.NewServer(dgproto.ServerConfig{
			StaticKey: key, MaxConcurrentHandshakes: 1, MaxActiveConnections: 1,
			Admission: func(ctx context.Context, _ *dgproto.Connection, _ dgproto.AdmissionInfo) error {
				admissionStats <- server.Stats() // callback must not hold the ownership lock
				select {
				case <-admissionGate:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			OnDisconnect: func(context.Context, *dgproto.Connection, dgproto.AdmissionInfo, error) {
				disconnectStats <- server.Stats()
				select {
				case <-disconnectGate:
				case <-time.After(connectionTestTimeout):
					t.Error("disconnect gate timed out")
				}
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		t.Cleanup(func() {
			releaseAdmission()
			releaseDisconnect()
			_ = server.Abort()
			closed := make(chan error, 1)
			go func() { closed <- server.Close() }()
			receiveBounded(t, closed)
		})
		check := func(got dgproto.ServerStats, h, c, pending, active int, shutdown bool) {
			t.Helper()
			want := dgproto.ServerStats{HandshakeSlotsUsed: h, HandshakeSlotsCapacity: 1,
				ConnectionSlotsUsed: c, ConnectionSlotsCapacity: 1,
				PendingConnections: pending, ActiveConnections: active, ShuttingDown: shutdown}
			if got != want {
				t.Fatalf("Stats = %+v, want %+v", got, want)
			}
		}

		// A silent peer occupies the handshake pool and pending socket map.
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		select {
		case listener.sockets <- b:
		case <-time.After(connectionTestTimeout):
			t.Fatal("stalled socket not accepted")
		}
		synctest.Wait()
		check(server.Stats(), 1, 0, 1, 0, false)
		_ = a.Close()
		synctest.Wait()
		check(server.Stats(), 0, 0, 0, 0, false)

		tr, _ := connectScriptedServer(t, listener, key, peer)
		check(receiveBounded(t, admissionStats), 0, 1, 1, 0, false)
		synctest.Wait()
		check(server.Stats(), 0, 1, 1, 0, false)
		releaseAdmission()
		synctest.Wait()
		check(server.Stats(), 0, 1, 0, 1, false)
		_ = tr.Close()
		check(receiveBounded(t, disconnectStats), 0, 1, 0, 0, false)
		synctest.Wait()
		check(server.Stats(), 0, 1, 0, 0, false)

		// Close has begun but must wait for the slot-owning disconnect callback.
		closed := make(chan error, 1)
		go func() { closed <- server.Close() }()
		synctest.Wait()
		check(server.Stats(), 0, 1, 0, 0, true)
		select {
		case <-closed:
			t.Fatal("Close returned through blocked hook")
		default:
		}
		releaseDisconnect()
		if err := receiveBounded(t, closed); err != nil {
			t.Fatal(err)
		}
		if err := receiveBounded(t, served); !errors.Is(err, dgproto.ErrServerClosed) {
			t.Fatalf("Serve = %v", err)
		}
		check(server.Stats(), 0, 0, 0, 0, true)
	})
}

func TestServerStatsConcurrentServeShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, err := dgproto.NewServer(dgproto.ServerConfig{StaticKey: serverTestKey(t, 4)})
		if err != nil {
			t.Fatal(err)
		}
		listener := &lifecycleListener{sockets: make(chan net.Conn), done: make(chan struct{})}
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		select {
		case listener.sockets <- b:
		case <-time.After(connectionTestTimeout):
			t.Fatal("socket not accepted")
		}
		synctest.Wait()
		start := make(chan struct{})
		sampled := make(chan struct{})
		go func() {
			<-start
			for range 1000 {
				got := server.Stats()
				if got.HandshakeSlotsUsed < 0 || got.HandshakeSlotsUsed > got.HandshakeSlotsCapacity || got.ConnectionSlotsUsed > got.ConnectionSlotsCapacity || got.ActiveConnections != 0 || got.PendingConnections > 1 {
					t.Errorf("invalid concurrent Stats: %+v", got)
				}
			}
			close(sampled)
		}()
		closed := make(chan error, 1)
		go func() { <-start; closed <- server.Close() }()
		close(start)
		if err := receiveBounded(t, closed); err != nil {
			t.Fatal(err)
		}
		receiveBounded(t, sampled)
		if err := receiveBounded(t, served); !errors.Is(err, dgproto.ErrServerClosed) {
			t.Fatal(err)
		}
		want := dgproto.ServerStats{HandshakeSlotsCapacity: 64, ConnectionSlotsCapacity: 1024, ShuttingDown: true}
		if got := server.Stats(); got != want {
			t.Fatalf("closed Stats = %+v", got)
		}
	})
}
