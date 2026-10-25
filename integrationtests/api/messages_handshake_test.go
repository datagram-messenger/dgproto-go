package dgproto_test

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

var (
	_ encoding.BinaryMarshaler   = dgproto.HandshakeInit{}
	_ encoding.BinaryMarshaler   = dgproto.HandshakeResponse{}
	_ encoding.BinaryMarshaler   = dgproto.HandshakeFinish{}
	_ encoding.BinaryUnmarshaler = (*dgproto.HandshakeInit)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.HandshakeResponse)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.HandshakeFinish)(nil)
)

// Moving source files must not replace public concrete types with internal aliases.
func TestHandshakePublicCompatibility(t *testing.T) {
	for _, value := range []any{dgproto.NoisePatternXX, dgproto.HandshakeInit{}, dgproto.HandshakeResponse{}, dgproto.HandshakeFinish{}} {
		typ := reflect.TypeOf(value)
		if typ.PkgPath() != "github.com/datagram-messenger/dgproto-go" {
			t.Fatalf("%s moved to %s", typ.Name(), typ.PkgPath())
		}
	}
	_, err := (dgproto.HandshakeInit{Pattern: dgproto.NoisePatternIK}).MarshalBinary()
	if !errors.Is(err, dgproto.ErrInvalidNoisePattern) {
		t.Fatalf("pattern sentinel lost: %v", err)
	}
	var wrapped interface{ Unwrap() error }
	if !errors.As(err, &wrapped) || wrapped.Unwrap() != dgproto.ErrInvalidNoisePattern {
		t.Fatalf("pattern error wrapping changed: %v", err)
	}
	_, err = (dgproto.HandshakeFinish{}).MarshalBinary()
	if !errors.Is(fmt.Errorf("consumer: %w", err), dgproto.ErrMessageLength) {
		t.Fatalf("length sentinel lost: %v", err)
	}
}

func TestHandshakeFinishOwnershipAndRejectedInput(t *testing.T) {
	payload := bytes.Repeat([]byte{5}, dgproto.HandshakeFinishFixedSize)
	wire, err := (dgproto.HandshakeFinish{NoisePayload: payload}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 9
	if wire[0] != 5 {
		t.Fatal("marshal aliases caller payload")
	}
	var got dgproto.HandshakeFinish
	if err := got.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	wire[0] = 7
	if got.NoisePayload[0] != 5 {
		t.Fatal("unmarshal aliases input")
	}
	before := append([]byte(nil), got.NoisePayload...)
	for n := 0; n <= dgproto.HandshakeFinishFixedSize+1; n++ {
		if n == dgproto.HandshakeFinishFixedSize {
			continue
		}
		bad := make([]byte, n)
		if err := got.UnmarshalBinary(bad); !errors.Is(err, dgproto.ErrMessageLength) {
			t.Fatalf("length %d: %v", n, err)
		}
		if !bytes.Equal(got.NoisePayload, before) {
			t.Fatalf("length %d mutated destination", n)
		}
		if _, err := (dgproto.HandshakeFinish{NoisePayload: bad}).MarshalBinary(); !errors.Is(err, dgproto.ErrMessageLength) {
			t.Fatalf("marshal length %d: %v", n, err)
		}
	}
}
