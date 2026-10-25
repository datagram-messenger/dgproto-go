package integrationtests

import (
	"context"
	"net"
	"sync"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Synthetic directional secrets avoid handshake work in established-runtime tests.
func runtimeSessions(t *testing.T) (*dgproto.Session, *dgproto.Session) {
	t.Helper()
	secrets := dgproto.HandshakeSecrets{SessionID: [16]byte{1}, SendKey: [32]byte{2}, ReceiveKey: [32]byte{3}}
	a, err := dgproto.NewSession(dgproto.CipherChaCha20Poly1305, secrets)
	if err != nil {
		t.Fatal(err)
	}
	secrets.SendKey, secrets.ReceiveKey = secrets.ReceiveKey, secrets.SendKey
	b, err := dgproto.NewSession(dgproto.CipherChaCha20Poly1305, secrets)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

func runtimeConnection(t *testing.T, socket net.Conn, session *dgproto.Session, config dgproto.ConnectionConfig) *dgproto.Connection {
	t.Helper()
	c := dgproto.NewConnection(dgproto.NewTCPTransport(socket), session, config)
	t.Cleanup(func() { _ = socket.Close(); _ = c.Close(); receiveBounded(t, c.Done()) })
	return c
}

// Enqueue evaluates Done once. The write-completion select evaluates it again;
// this observation proves acceptance without accessing a private queue.
type completionContext struct {
	context.Context
	mu       sync.Mutex
	calls    int
	accepted chan struct{}
}

func (c *completionContext) Done() <-chan struct{} {
	c.mu.Lock()
	c.calls++
	if c.calls == 2 {
		close(c.accepted)
	}
	c.mu.Unlock()
	return c.Context.Done()
}

func readRuntimeAck(t *testing.T, tr *dgproto.TCPTransport, session *dgproto.Session, want uint64) dgproto.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	f, err := tr.ReadFrame(ctx)
	if err != nil {
		t.Fatal(err)
	}
	message, err := session.Receive(f)
	if err != nil {
		t.Fatal(err)
	}
	ack, ok := message.(*dgproto.Ack)
	if !ok || len(ack.Sequences) != 1 || ack.Sequences[0] != want {
		t.Fatalf("message = %#v, want Ack(%d)", message, want)
	}
	return f
}
