package dgproto_test

import (
	"bytes"
	"encoding"
	"errors"
	"reflect"
	"strings"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

var (
	_ encoding.BinaryMarshaler   = dgproto.EncryptedData{}
	_ encoding.BinaryMarshaler   = dgproto.SessionClose{}
	_ encoding.BinaryUnmarshaler = (*dgproto.EncryptedData)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.SessionClose)(nil)
)

func TestTLVMessagePublicIdentityAndOwnership(t *testing.T) {
	for _, value := range []any{dgproto.EncryptedData{}, dgproto.SessionClose{}} {
		if got := reflect.TypeOf(value).PkgPath(); got != "github.com/datagram-messenger/dgproto-go" {
			t.Fatalf("%T has package %q", value, got)
		}
	}
	input := []byte{0x34, 0x12, 7, 0, 0xfe, 1, 0, 9, 1, 0, 0, 0xa5}
	var data dgproto.EncryptedData
	if err := data.UnmarshalBinary(input); err != nil {
		t.Fatal(err)
	}
	input[7] = 8
	if data.StreamID != 0x1234 || data.AppMessageType != 7 || data.Fields[0].Type != 0xfe || data.Fields[0].Value[0] != 9 || data.Fields[1].Value != nil {
		t.Fatalf("decoded ownership/shape: %#v", data)
	}
	wire, err := data.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	data.Fields[0].Value[0] = 6
	if wire[7] != 9 || wire[11] != 0 {
		t.Fatal("encode aliases fields or fails to zero TLV padding")
	}
	if err := data.UnmarshalBinary([]byte{0, 0, 0, 0}); err != nil || data.Fields == nil || len(data.Fields) != 0 {
		t.Fatalf("empty decoded fields shape: %#v, %v", data.Fields, err)
	}
	textWire := []byte{3, 0, 1, 2, 0, 'h', 'i', 0xa5, 0xa5, 0xa5}
	var close dgproto.SessionClose
	if err := close.UnmarshalBinary(textWire); err != nil {
		t.Fatal(err)
	}
	textWire[5] = 'x'
	if close.Code != 3 || close.Reason != "hi" {
		t.Fatal("close decode did not own its string")
	}
	encoded, err := close.MarshalBinary()
	if err != nil || !bytes.Equal(encoded, []byte{3, 0, 1, 2, 0, 'h', 'i', 0, 0, 0}) {
		t.Fatalf("unchanged close encoding: % x, %v", encoded, err)
	}
}

func TestTLVMessageValidationPriorityAndUnchangedDestination(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    []byte
		newValue func() encoding.BinaryUnmarshaler
		want     error
	}{
		{"application short", []byte{0, 0, 0}, func() encoding.BinaryUnmarshaler { return &dgproto.EncryptedData{StreamID: 9} }, dgproto.ErrMessageTooShort},
		{"application reserved before TLV", []byte{0, 0, 0, 1, 1}, func() encoding.BinaryUnmarshaler { return &dgproto.EncryptedData{StreamID: 9} }, dgproto.ErrMessageReserved},
		{"application malformed before duplicate", []byte{0, 0, 0, 0, 1, 0, 0, 0, 1}, func() encoding.BinaryUnmarshaler { return &dgproto.EncryptedData{StreamID: 9} }, dgproto.ErrTLVTooShort},
		{"text duplicate before unknown and code", []byte{4, 0, 2, 0, 0, 0, 2, 0, 0, 0}, func() encoding.BinaryUnmarshaler { return &dgproto.SessionClose{Code: 3, Reason: "old"} }, dgproto.ErrDuplicateMessageTLV},
		{"text unknown before code", []byte{4, 0, 2, 0, 0, 0}, func() encoding.BinaryUnmarshaler { return &dgproto.SessionClose{Code: 3, Reason: "old"} }, dgproto.ErrUnknownMessageTLV},
		{"text UTF8 before code", []byte{4, 0, 1, 1, 0, 0xff}, func() encoding.BinaryUnmarshaler { return &dgproto.SessionClose{Code: 3, Reason: "old"} }, dgproto.ErrInvalidUTF8},
		{"text close code", []byte{4, 0}, func() encoding.BinaryUnmarshaler { return &dgproto.SessionClose{Code: 3, Reason: "old"} }, dgproto.ErrInvalidCloseCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.newValue()
			before := reflect.ValueOf(value).Elem().Interface()
			if err := value.UnmarshalBinary(tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if !reflect.DeepEqual(before, reflect.ValueOf(value).Elem().Interface()) {
				t.Fatal("destination changed on error")
			}
		})
	}
	tooLongInvalid := strings.Repeat("a", dgproto.MaxReasonSize) + "\xff"
	if _, err := (dgproto.SessionClose{Code: 4, Reason: tooLongInvalid}).MarshalBinary(); !errors.Is(err, dgproto.ErrInvalidCloseCode) {
		t.Fatalf("close code must precede text validation: %v", err)
	}
	if _, err := (dgproto.ErrorMessage{Context: tooLongInvalid}).MarshalBinary(); !errors.Is(err, dgproto.ErrInvalidUTF8) {
		t.Fatalf("UTF8 must precede size validation: %v", err)
	}
	fields := []dgproto.TLV{{Type: 9, Value: make([]byte, dgproto.MaxTLVValueSize+1)}, {Type: 9}}
	if _, err := (dgproto.EncryptedData{Fields: fields}).MarshalBinary(); !errors.Is(err, dgproto.ErrDuplicateMessageTLV) {
		t.Fatalf("duplicate must precede field size: %v", err)
	}
}

func TestTLVMessageCurrentAndNilSentinels(t *testing.T) {
	// Never parallel: writable root sentinels are part of the existing API.
	original := dgproto.ErrMessageTooShort
	defer func() { dgproto.ErrMessageTooShort = original }()
	replacement := &messageSentinel{"replacement"}
	dgproto.ErrMessageTooShort = replacement
	value := dgproto.ErrorMessage{Code: 9, Context: "old"}
	err := value.UnmarshalBinary(nil)
	var typed *messageSentinel
	if err != replacement || !errors.Is(err, replacement) || !errors.As(err, &typed) || typed != replacement || value.Context != "old" {
		t.Fatalf("current sentinel/unchanged destination: %#v, %v", value, err)
	}
	dgproto.ErrMessageTooShort = nil
	// A nil replacement turns the helper's direct short-input return into
	// success; both existing wrappers then assign the helper's zero result.
	if err := value.UnmarshalBinary(nil); err != nil || value != (dgproto.ErrorMessage{}) {
		t.Fatalf("nil sentinel error destination: %#v, %v", value, err)
	}
	close := dgproto.SessionClose{Code: 3, Reason: "old"}
	if err := close.UnmarshalBinary(nil); err != nil || close != (dgproto.SessionClose{}) {
		t.Fatalf("nil sentinel close destination: %#v, %v", close, err)
	}
}

func TestTLVMessageNilReceivers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value encoding.BinaryUnmarshaler
		valid []byte
	}{
		{"application", (*dgproto.EncryptedData)(nil), []byte{0, 0, 0, 0}},
		{"close", (*dgproto.SessionClose)(nil), []byte{0, 0}},
		{"error", (*dgproto.ErrorMessage)(nil), []byte{0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.value.UnmarshalBinary(nil); !errors.Is(err, dgproto.ErrMessageTooShort) {
				t.Fatalf("invalid input on nil receiver: %v", err)
			}
			defer func() {
				if recover() == nil {
					t.Fatal("valid input on nil receiver must retain assignment panic")
				}
			}()
			_ = tc.value.UnmarshalBinary(tc.valid)
		})
	}
}
