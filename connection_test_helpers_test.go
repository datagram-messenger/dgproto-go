package dgproto

import (
	"context"
	"net"
	"testing"
	"time"
)

const connectionTestTimeout = time.Second

// Shared by Connection and Server white-box lifecycle/queue owners.
func receiveBounded[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(connectionTestTimeout):
		t.Fatal("operation did not complete")
		var zero T
		return zero
	}
}

func testConnectionPair(t *testing.T, config ConnectionConfig) (*Connection, *Session, *TCPTransport, func()) {
	t.Helper()
	localConn, peerConn := net.Pipe()
	localSession, peerSession := testSessions(t)
	connection := NewConnection(NewTCPTransport(localConn), localSession, config)
	peerTransport := NewTCPTransport(peerConn)
	cleanup := func() {
		_ = peerTransport.Close()
		_ = connection.Close()
		select {
		case <-connection.Done():
		case <-time.After(connectionTestTimeout):
			t.Fatal("connection did not stop")
		}
	}
	return connection, peerSession, peerTransport, cleanup
}

func writeConnectionMessage(t *testing.T, transport *TCPTransport, session *Session, message any) {
	t.Helper()
	for {
		frame, err := session.Send(message, 0)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
		err = transport.WriteFrame(ctx, frame)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if frame.Header.MessageType != MessageTypeRekeyInit {
			return
		}
		if err := session.MarkRekeySent(frame); err != nil {
			t.Fatal(err)
		}
	}
}

func readConnectionMessage(t *testing.T, transport *TCPTransport, session *Session) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	frame, err := transport.ReadFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	message, err := session.Receive(frame)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func waitConnection(t *testing.T, connection *Connection) {
	t.Helper()
	select {
	case <-connection.Done():
	case <-time.After(connectionTestTimeout):
		t.Fatal("connection did not stop")
	}
}
