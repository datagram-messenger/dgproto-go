package integrationtests

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestTCPTransportRejectsOversizedHeaderBeforeBody(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	wire := make([]byte, dgproto.HeaderSize)
	copy(wire[:4], dgproto.Magic[:])
	wire[4] = dgproto.Version
	wire[6] = byte(dgproto.MessageTypeEncryptedData)
	binary.LittleEndian.PutUint32(wire[32:36], dgproto.MaxFrameSize)
	go func() { _, _ = b.Write(wire) }()
	_, err := dgproto.NewTCPTransport(a).ReadFrame(context.Background())
	if !errors.Is(err, dgproto.ErrFrameTooLarge) {
		t.Fatalf("error = %v, want %v", err, dgproto.ErrFrameTooLarge)
	}
}

func TestTCPTransportEOFAndMalformedFrame(t *testing.T) {
	t.Run("EOF", func(t *testing.T) {
		a, b := net.Pipe()
		b.Close()
		defer a.Close()
		_, err := dgproto.NewTCPTransport(a).ReadFrame(context.Background())
		if !errors.Is(err, io.EOF) {
			t.Fatalf("error = %v, want EOF", err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		wire := make([]byte, dgproto.HeaderSize)
		copy(wire, []byte("BAD!"))
		go func() { _, _ = b.Write(wire) }()
		_, err := dgproto.NewTCPTransport(a).ReadFrame(context.Background())
		if !errors.Is(err, dgproto.ErrInvalidMagic) {
			t.Fatalf("error = %v, want %v", err, dgproto.ErrInvalidMagic)
		}
	})
}

func TestTCPTransportContextCancellation(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := dgproto.NewTCPTransport(a).ReadFrame(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}
