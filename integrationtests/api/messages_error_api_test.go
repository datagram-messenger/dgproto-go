package dgproto_test

import (
	"bytes"
	"encoding"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

var (
	_ encoding.BinaryMarshaler   = dgproto.ErrorMessage{}
	_ encoding.BinaryUnmarshaler = (*dgproto.ErrorMessage)(nil)
)

func TestErrorMessageNormativeRaw(t *testing.T) {
	if reflect.TypeOf(dgproto.ErrorMessage{}).PkgPath() != "github.com/datagram-messenger/dgproto-go" {
		t.Fatal("public identity changed")
	}
	for _, tc := range []struct {
		code       uint16
		text, wire string
	}{
		{9, "oops", "09006f6f7073"},
		{255, "Ошибка", "ff00d09ed188d0b8d0b1d0bad0b0"},
		{3, "hi", "03006869"},
		{0x1234, "hi", "34126869"},
		{0, "a\x00b", "0000610062"},
		{9, "", "0900"},
	} {
		want, _ := hex.DecodeString(tc.wire)
		value := dgproto.ErrorMessage{Code: tc.code, Context: tc.text}
		got, err := value.MarshalBinary()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("code %d: got %x, %v; want %x", tc.code, got, err, want)
		}
		var decoded dgproto.ErrorMessage
		if err := decoded.UnmarshalBinary(want); err != nil || decoded != value {
			t.Fatalf("decode %x: %#v, %v", want, decoded, err)
		}
		for i := range want {
			want[i] = 0xff
		}
		if decoded != value {
			t.Fatal("decoded string aliases input")
		}
		got[0] ^= 0xff
		again, err := value.MarshalBinary()
		if err != nil || again[0] != byte(tc.code) {
			t.Fatal("marshal storage not independently owned")
		}
	}
	// Old TLV bytes can be valid raw UTF-8: no heuristic fallback or rejection.
	var old dgproto.ErrorMessage
	if err := old.UnmarshalBinary([]byte{3, 0, 1, 2, 0, 'h', 'i', 0, 0, 0}); err != nil || old.Context != "\x01\x02\x00hi\x00\x00\x00" {
		t.Fatalf("raw-only decoding: %#v, %v", old, err)
	}
}

func TestErrorMessageRawValidation(t *testing.T) {
	const maxContext = 65477 // literal frame budget, independent of implementation
	for _, tc := range []struct {
		wire []byte
		want error
	}{
		{nil, dgproto.ErrMessageTooShort}, {[]byte{1}, dgproto.ErrMessageTooShort},
		{[]byte{4, 0, 0xff}, dgproto.ErrInvalidUTF8},
		{append([]byte{4, 0}, bytes.Repeat([]byte{0xff}, maxContext+1)...), dgproto.ErrMessageLength},
	} {
		value := dgproto.ErrorMessage{Code: 9, Context: "old"}
		before := value
		if err := value.UnmarshalBinary(tc.wire); !errors.Is(err, tc.want) || value != before {
			t.Fatalf("validation/atomic destination: %#v, %v; want %v", value, err, tc.want)
		}
	}
	text := strings.Repeat("a", maxContext)
	wire, err := (dgproto.ErrorMessage{Context: text}).MarshalBinary()
	if err != nil || len(wire) != 65479 {
		t.Fatalf("exact maximum: %d, %v", len(wire), err)
	}
	var decoded dgproto.ErrorMessage
	if err := decoded.UnmarshalBinary(wire); err != nil || decoded.Context != text {
		t.Fatalf("max decode: %v", err)
	}
	if _, err := (dgproto.ErrorMessage{Context: text + "a"}).MarshalBinary(); !errors.Is(err, dgproto.ErrReasonTooLong) {
		t.Fatalf("size: %v", err)
	}
	if _, err := (dgproto.ErrorMessage{Context: text + "\xff"}).MarshalBinary(); !errors.Is(err, dgproto.ErrInvalidUTF8) {
		t.Fatalf("UTF8 before size: %v", err)
	}
	// The limit counts bytes, not runes.
	if _, err := (dgproto.ErrorMessage{Context: strings.Repeat("я", maxContext/2) + "a"}).MarshalBinary(); err != nil {
		t.Fatal(err)
	}
	if _, err := (dgproto.ErrorMessage{Context: strings.Repeat("я", maxContext/2+1)}).MarshalBinary(); !errors.Is(err, dgproto.ErrReasonTooLong) {
		t.Fatalf("multibyte limit: %v", err)
	}
}

func TestErrorMessageCurrentAndNilSentinels(t *testing.T) {
	// Serial: public mutable sentinel contract.
	original := dgproto.ErrMessageTooShort
	defer func() { dgproto.ErrMessageTooShort = original }()
	replacement := &messageSentinel{"replacement"}
	dgproto.ErrMessageTooShort = replacement
	value := dgproto.ErrorMessage{Code: 9, Context: "old"}
	err := value.UnmarshalBinary(nil)
	var typed *messageSentinel
	if err != replacement || !errors.Is(err, replacement) || !errors.As(err, &typed) || typed != replacement || value.Context != "old" {
		t.Fatalf("current sentinel: %#v, %v", value, err)
	}
	dgproto.ErrMessageTooShort = nil
	if err := value.UnmarshalBinary(nil); err != nil || value != (dgproto.ErrorMessage{}) {
		t.Fatalf("nil sentinel zeros: %#v, %v", value, err)
	}
}

func TestErrorMessageNilReceiver(t *testing.T) {
	var value *dgproto.ErrorMessage
	if err := value.UnmarshalBinary(nil); !errors.Is(err, dgproto.ErrMessageTooShort) {
		t.Fatalf("invalid input: %v", err)
	}
	if err := value.UnmarshalBinary([]byte{0, 0, 0xff}); !errors.Is(err, dgproto.ErrInvalidUTF8) {
		t.Fatalf("invalid UTF8 on nil receiver: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("valid nil receiver must retain assignment panic")
		}
	}()
	_ = value.UnmarshalBinary([]byte{0, 0})
}
