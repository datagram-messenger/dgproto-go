package dgproto

import (
	"net"
	"sync"
	"testing"
)

func testTransportFrame(t *testing.T, sequence uint64, payload []byte) Frame {
	t.Helper()
	f, err := NewFrame(MessageTypeEncryptedData, [16]byte{1}, sequence, payload, make([]byte, AEADTagSize), nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// gatedIOConn remains root-local for the white-box write-completion regression.
// The public TCP cancellation suite owns its separate package-local fixture.
type gatedIOConn struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
	write   bool
}

func (c *gatedIOConn) Read(p []byte) (int, error) {
	if !c.write {
		c.once.Do(func() { close(c.entered) })
	}
	return c.Conn.Read(p)
}
func (c *gatedIOConn) Write(p []byte) (int, error) {
	if c.write {
		c.once.Do(func() { close(c.entered) })
	}
	return c.Conn.Write(p)
}
