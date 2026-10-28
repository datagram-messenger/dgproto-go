package integrationtests

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Accept is scripted, but handshakes, encrypted traffic and Server ownership are
// real. synctest.Wait establishes callback completion and capacity release;
// peer EOF alone would not establish that boundary.
type lifecycleListener struct {
	sockets chan net.Conn
	done    chan struct{}
	once    sync.Once
}

func (l *lifecycleListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.sockets:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *lifecycleListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *lifecycleListener) Addr() net.Addr { return &net.TCPAddr{} }

type admissionEvent struct {
	conn *dgproto.Connection
	info dgproto.AdmissionInfo
}
type disconnectEvent struct {
	admissionEvent
	cause      error
	background bool
}

func serverTestKey(t *testing.T, value byte) dgproto.StaticKey {
	t.Helper()
	key, err := dgproto.LoadStaticKey(bytes.Repeat([]byte{value}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func connectScriptedServer(t *testing.T, l *lifecycleListener, serverKey, clientKey dgproto.StaticKey) (*dgproto.TCPTransport, *dgproto.Session) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	select {
	case l.sockets <- b:
	case <-l.done:
		t.Fatal("listener closed")
	case <-time.After(connectionTestTimeout):
		t.Fatal("server did not accept")
	}
	tr := dgproto.NewTCPTransport(a)
	hs, err := dgproto.NewInitiatorHandshake(clientKey, serverKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	for flight := 0; flight < 2; flight++ {
		payload, err := hs.WriteFlight()
		if err != nil {
			t.Fatal(err)
		}
		kind := dgproto.MessageTypeHandshakeInit
		if flight == 1 {
			kind = dgproto.MessageTypeHandshakeResponse
		}
		frame, err := dgproto.NewFrame(kind, [16]byte{}, 0, payload, make([]byte, dgproto.AEADTagSize), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.WriteFrame(ctx, frame); err != nil {
			t.Fatal(err)
		}
		if flight == 0 {
			response, err := tr.ReadFrame(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := hs.ReadFlight(response.Payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := hs.Result()
	if err != nil {
		t.Fatal(err)
	}
	session, err := dgproto.NewSessionFromHandshake(dgproto.CipherChaCha20Poly1305, result)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return tr, session
}
