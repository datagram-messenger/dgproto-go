package dgproto

import (
	"net"
	"testing"
)

// Public stream and cancellation tests live in integrationtests; constructor
// misuse requiring private Session construction remains in this root suite.

func TestNewConnectionNilPanics(t *testing.T) {
	key, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	transport := NewTCPTransport(a)

	t.Run("nil transport", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("NewConnection(nil, session, config) did not panic")
			}
		}()
		_ = key // suppress unused warning
		NewConnection(nil, &Session{}, ConnectionConfig{})
	})

	t.Run("nil session", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("NewConnection(transport, nil, config) did not panic")
			}
		}()
		NewConnection(transport, nil, ConnectionConfig{})
	})
}
