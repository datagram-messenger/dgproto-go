package integrationtests

import (
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

const connectionTestTimeout = time.Second

func testTransportFrame(t *testing.T, sequence uint64, payload []byte) dgproto.Frame {
	t.Helper()
	f, err := dgproto.NewFrame(dgproto.MessageTypeEncryptedData, [16]byte{1}, sequence, payload, make([]byte, dgproto.AEADTagSize), nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

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
