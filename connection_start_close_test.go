package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
)

func TestConnectionCloseBeforeStartCompletesDone(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	session, _ := testSessions(t)
	connection := NewConnection(NewTCPTransport(local), session, ConnectionConfig{})
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connection.Done():
	default:
		t.Fatal("Done remains open after closing a stopped connection")
	}
	connection.Start(context.Background())
	if connection.started.Load() {
		t.Fatal("Start launched a runtime after Close")
	}
	if !errors.Is(connection.Err(), ErrConnectionClosed) {
		t.Fatalf("terminal cause = %v", connection.Err())
	}
}

func TestConnectionConcurrentStartAndCloseCompletesDone(t *testing.T) {
	for range 64 {
		local, peer := net.Pipe()
		session, _ := testSessions(t)
		connection := NewConnection(NewTCPTransport(local), session, ConnectionConfig{})
		// Ensure a started runtime cannot block on peer I/O during this test.
		_ = peer.Close()
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			connection.Start(context.Background())
		})
		wg.Go(func() {
			<-start
			_ = connection.Close()
		})
		close(start)
		wg.Wait()
		waitConnection(t, connection)
	}
}
