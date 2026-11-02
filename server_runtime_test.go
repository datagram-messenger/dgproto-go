package dgproto

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestServerHandshakeAdmissionSaturationAndRelease(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxConcurrentHandshakes = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("failed to acquire first handshake slot")
	}
	if server.tryAcquire(server.handshakes) {
		t.Fatal("acquired saturated handshake slot")
	}
	server.release(server.handshakes)
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("handshake slot was not released")
	}
	server.release(server.handshakes)
}

func TestServerConnectionAdmissionSaturationAndRelease(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxActiveConnections = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("failed to acquire first connection slot")
	}
	if server.tryAcquire(server.connSlots) {
		t.Fatal("acquired saturated connection slot")
	}
	server.release(server.connSlots)
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("connection slot was not released")
	}
	server.release(server.connSlots)
}

func TestServerReleasesHandshakeSlotAfterFailure(t *testing.T) {
	server, err := NewServer(func() ServerConfig {
		c := validServerConfig(t)
		c.MaxConcurrentHandshakes = 1
		c.HandshakeTimeout = time.Second
		return c
	}())
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	if !server.tryAcquire(server.handshakes) || !server.registerHandshake(serverConn) {
		t.Fatal("failed to admit handshake")
	}
	done := make(chan struct{})
	go func() {
		server.handleConn(serverConn)
		close(done)
	}()
	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed handshake did not finish")
	}
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("failed handshake did not release slot")
	}
	server.release(server.handshakes)
}

func TestServerReleasesConnectionSlotAfterClose(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxActiveConnections = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("failed to acquire connection slot")
	}
	server.release(server.connSlots)
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("closed connection did not release slot")
	}
	server.release(server.connSlots)
}

func TestHardeningHandoffShutdownStress(t *testing.T) {
	for range 20 {
		s, err := NewServer(validServerConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		a, b := net.Pipe()
		session, _ := testSessions(t)
		c := NewConnection(NewTCPTransport(a), session, ConnectionConfig{})
		if !s.registerHandshake(a) {
			t.Fatal("registration rejected")
		}
		start := make(chan struct{})
		registered := make(chan bool, 1)
		shutdown := make(chan struct{})
		go func() { <-start; registered <- s.registerConn(c) }()
		go func() { <-start; _ = s.Abort(); close(shutdown) }()
		close(start)
		receiveBounded(t, registered)
		receiveBounded(t, shutdown)
		// Whichever side wins handoff, Abort must own and close the socket.
		ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
		_, err = NewTCPTransport(b).ReadFrame(ctx)
		cancel()
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("handoff lost socket")
		}
		s.unregisterHandshake(a)
		s.unregisterConn(c)
		s.wg.Done()
		_ = c.Close()
		_ = b.Close()
		_ = s.Close()
	}
}

// Cover both serialized outcomes of the handoff/shutdown lock alongside stress.
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
