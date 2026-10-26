package integrationtests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func framedBytes(t *testing.T, f dgproto.Frame) []byte {
	t.Helper()
	b, err := f.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTCPTransportFragmentedAndCoalescedReads(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	tr := dgproto.NewTCPTransport(client)
	first := testTransportFrame(t, 1, []byte("one"))
	second := testTransportFrame(t, 2, []byte("two"))
	wire := append(framedBytes(t, first), framedBytes(t, second)...)
	go func() {
		for _, b := range wire {
			_, _ = server.Write([]byte{b})
		}
	}()
	for _, want := range []dgproto.Frame{first, second} {
		got, err := tr.ReadFrame(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

type shortConn struct {
	net.Conn
	max int
}

func (c shortConn) Write(p []byte) (int, error) {
	if len(p) > c.max {
		p = p[:c.max]
	}
	return c.Conn.Write(p)
}

func TestTCPTransportPartialWritesUseCanonicalWireFormat(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	writer := dgproto.NewTCPTransport(shortConn{Conn: a, max: 3})
	want := testTransportFrame(t, 7, bytes.Repeat([]byte{9}, 100))
	wantWire := framedBytes(t, want)
	errCh := make(chan error, 1)
	go func() { errCh <- writer.WriteFrame(context.Background(), want) }()

	gotWire := make([]byte, len(wantWire))
	if _, err := io.ReadFull(b, gotWire); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatal("wire frame mismatch")
	}
	if !bytes.Equal(gotWire[:4], dgproto.Magic[:]) {
		t.Fatalf("first four bytes = %q, want DGP1 magic", gotWire[:4])
	}
}

func TestTCPTransportTruncatedFrame(t *testing.T) {
	a, b := net.Pipe()
	want := testTransportFrame(t, 9, []byte("truncated"))
	wire := framedBytes(t, want)
	go func() {
		_, _ = b.Write(wire[:len(wire)-1])
		_ = b.Close()
	}()
	defer a.Close()
	_, err := dgproto.NewTCPTransport(a).ReadFrame(context.Background())
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestTCPTransportConcurrentWritesSerialized(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	tr := dgproto.NewTCPTransport(shortConn{Conn: a, max: 1})
	reader := dgproto.NewTCPTransport(b)
	const count = 12
	var wg sync.WaitGroup
	errCh := make(chan error, count)
	for i := range count {
		wg.Go(func() {
			errCh <- tr.WriteFrame(context.Background(), testTransportFrame(t, uint64(i), bytes.Repeat([]byte{byte(i)}, 32)))
		})
	}
	seen := make(map[uint64]bool)
	for range count {
		f, err := reader.ReadFrame(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		seen[f.Header.Sequence] = true
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != count {
		t.Fatalf("read %d distinct frames, want %d", len(seen), count)
	}
}

func TestNewTCPTransportNilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewTCPTransport(nil) did not panic")
		}
	}()
	dgproto.NewTCPTransport(nil)
}
