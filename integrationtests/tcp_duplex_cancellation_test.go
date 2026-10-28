package integrationtests

import (
	"context"
	"errors"
	"net"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Cancel one active lane while the opposite lane is also blocked in I/O.
// Same-lane stale-deadline tests alone cannot detect cross-direction deadlines.
func TestTCPDuplexCancellationIsDirectionLocal(t *testing.T) {
	for _, cancelWrite := range []bool{false, true} {
		name := "cancel read preserves write"
		if cancelWrite {
			name = "cancel write preserves read"
		}
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			readEntered, writeEntered := make(chan struct{}), make(chan struct{})
			// Reuse direction-specific gates; nested wrappers signal both lanes.
			socket := &gatedIOConn{Conn: &gatedIOConn{Conn: a, entered: readEntered}, entered: writeEntered, write: true}
			tr, peer := dgproto.NewTCPTransport(socket), dgproto.NewTCPTransport(b)
			readCtx, stopRead := context.WithCancel(context.Background())
			writeCtx, stopWrite := context.WithCancel(context.Background())
			defer stopRead()
			defer stopWrite()
			frame := testTransportFrame(t, 7, []byte("duplex"))
			readResult, writeResult := make(chan error, 1), make(chan error, 1)
			go func() {
				got, err := tr.ReadFrame(readCtx)
				if err == nil && (got.Header != frame.Header || string(got.Payload) != string(frame.Payload)) {
					err = errors.New("opposite read changed frame")
				}
				readResult <- err
			}()
			go func() { writeResult <- tr.WriteFrame(writeCtx, frame) }()
			receiveBounded(t, readEntered)
			receiveBounded(t, writeEntered)
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			if cancelWrite {
				stopWrite()
				if err := receiveBounded(t, writeResult); !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled write = %v", err)
				}
				if err := peer.WriteFrame(ctx, frame); err != nil {
					t.Fatal(err)
				}
				if err := receiveBounded(t, readResult); err != nil {
					t.Fatalf("write cancellation poisoned read: %v", err)
				}
			} else {
				stopRead()
				if err := receiveBounded(t, readResult); !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled read = %v", err)
				}
				got, err := peer.ReadFrame(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if got.Header != frame.Header || string(got.Payload) != string(frame.Payload) {
					t.Fatal("opposite write changed frame")
				}
				if err := receiveBounded(t, writeResult); err != nil {
					t.Fatalf("read cancellation poisoned write: %v", err)
				}
			}
		})
	}
}
