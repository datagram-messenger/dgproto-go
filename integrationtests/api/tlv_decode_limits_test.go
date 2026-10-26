package dgproto_test

import (
	. "github.com/datagram-messenger/dgproto-go"

	"bytes"
	"errors"
	"testing"
)

func TestDecodeTLVAtomicMalformedSuffix(t *testing.T) {
	prefix := []byte{0xfe, 1, 0, 7, 0xfe, 0, 0, 0xa5}
	for _, suffix := range [][]byte{{1}, {1, 0}, {1, 2, 0, 7}, {1, 2, 0, 7, 8}} {
		wire := append(append([]byte(nil), prefix...), suffix...)
		got, err := DecodeTLVs(wire, 0)
		if err == nil || got != nil {
			t.Fatalf("malformed suffix returned partial output: %v, %v", got, err)
		}
	}
}

func TestDecodeTLVExactShapeOwnership(t *testing.T) {
	fields := []TLV{{Type: 0xfe, Value: bytes.Repeat([]byte{7}, 64000)}, {Type: 0xfe, Value: []byte{8, 9}}, {Type: 1}}
	wire, err := EncodeTLVs(fields)
	if err != nil {
		t.Fatal(err)
	}
	wire[len(wire)-1] = 0xa5 // opaque empty-field alignment padding
	got, err := DecodeTLVs(wire, len(wire))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Type != 0xfe || got[1].Type != 0xfe || got[2].Value != nil {
		t.Fatal("shape changed")
	}
	wire[3] = 1
	if got[0].Value[0] != 7 {
		t.Fatal("decoded value aliases input")
	}
	got[0].Value[0] = 2
	if wire[3] != 1 || got[1].Value[0] != 8 {
		t.Fatal("output mutation aliases other storage")
	}
	out, err := DecodeTLVs(wire, len(wire)-1)
	if out != nil || !errors.Is(err, ErrTLVDecodeLimit) {
		t.Fatalf("caller limit: %v, %v", out, err)
	}
	out, err = DecodeTLVs(make([]byte, MaxTLVSequenceSize+1), 0)
	if out != nil || !errors.Is(err, ErrTLVSequenceLimit) {
		t.Fatalf("protocol limit: %v, %v", out, err)
	}
	dense := bytes.Repeat([]byte{1, 0, 0, 0xa5}, MaxTLVElements)
	out, err = DecodeTLVs(dense, 0)
	if err != nil || len(out) != MaxTLVElements {
		t.Fatalf("dense boundary: %d, %v", len(out), err)
	}
}
