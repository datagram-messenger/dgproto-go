package dgproto_test

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"testing"

	dg "github.com/datagram-messenger/dgproto-go"
)

var _ encoding.BinaryMarshaler = dg.TLV{}
var _ func(uint8, []byte) (dg.TLV, error) = dg.NewTLV
var _ func([]dg.TLV) ([]byte, error) = dg.EncodeTLVs
var _ func([]byte, int) ([]dg.TLV, error) = dg.DecodeTLVs

type tlvSentinel struct{}

func (*tlvSentinel) Error() string { return "replacement" }

func TestTLVPublicIdentityAndEmptyOwnership(t *testing.T) {
	typ := reflect.TypeOf(dg.TLV{})
	if typ.Name() != "TLV" || typ.PkgPath() != "github.com/datagram-messenger/dgproto-go" || typ.NumField() != 2 || typ.Field(0).Name != "Type" || typ.Field(0).Type.Kind() != reflect.Uint8 || typ.Field(1).Name != "Value" || typ.Field(1).Type != reflect.TypeOf([]byte(nil)) {
		t.Fatalf("concrete type changed: %v", typ)
	}
	if dg.TLVHeaderSize != 3 || dg.MaxTLVValueSize != 65535 || dg.MaxTLVSequenceSize != dg.MaxFrameSize || dg.MaxTLVElements != 16383 {
		t.Fatal("public constants changed")
	}
	for _, input := range [][]byte{nil, {}} {
		field, err := dg.NewTLV(255, input)
		if err != nil || field.Value != nil {
			t.Fatal("empty NewTLV value must be nil")
		}
		fields, err := dg.DecodeTLVs(input, 0)
		if err != nil || fields == nil || len(fields) != 0 {
			t.Fatal("empty decode must be non-nil")
		}
	}
	for _, fields := range [][]dg.TLV{nil, {}} {
		wire, err := dg.EncodeTLVs(fields)
		if err != nil || wire == nil || len(wire) != 0 {
			t.Fatal("empty encode must be non-nil")
		}
	}
	input := []byte{1, 2, 3}
	field, _ := dg.NewTLV(255, input)
	input[0] = 7
	wire, _ := dg.EncodeTLVs([]dg.TLV{field, field, {Type: 255}})
	got, err := dg.DecodeTLVs(wire, 0)
	if err != nil || got[2].Value != nil {
		t.Fatal("empty decoded values must be nil")
	}
	wire[3] = 8
	got[0].Value[0] = 9
	if field.Value[0] != 1 || input[0] != 7 || got[1].Value[0] != 1 || wire[3] != 8 {
		t.Fatal("caller/output/value ownership changed")
	}
}

// No parallel tests: changing exported error variables requires exclusive use,
// just as it did before extraction. Include replacement and nil sentinels.
func TestTLVCurrentSentinelsAndExactDiagnostics(t *testing.T) {
	large := make([]byte, dg.MaxTLVValueSize+1)
	tests := []struct {
		name     string
		sentinel *error
		run      func() error
		format   string
		args     []any
	}{
		{"header", &dg.ErrTLVTooShort, func() error { _, e := dg.DecodeTLVs([]byte{1}, 0); return e }, "%w at offset %d: got %d header bytes", []any{0, 1}},
		{"value", &dg.ErrTLVTruncated, func() error { _, e := dg.DecodeTLVs([]byte{1, 2, 0, 7}, 0); return e }, "%w at offset %d: value needs %d bytes, got %d", []any{0, 2, 1}},
		{"padding", &dg.ErrTLVTruncated, func() error { _, e := dg.DecodeTLVs([]byte{1, 2, 0, 7, 8}, 0); return e }, "%w at offset %d: padding needs %d bytes, got %d", []any{0, 3, 0}},
		{"new", &dg.ErrTLVValueTooLarge, func() error { _, e := dg.NewTLV(1, large); return e }, "%w: got %d bytes", []any{len(large)}},
		{"len", &dg.ErrTLVValueTooLarge, func() error { _, e := (dg.TLV{Value: large}).EncodedLen(); return e }, "%w: got %d bytes", []any{len(large)}},
		{"marshal", &dg.ErrTLVValueTooLarge, func() error { _, e := (dg.TLV{Value: large}).MarshalBinary(); return e }, "%w: got %d bytes", []any{len(large)}},
		{"encode value", &dg.ErrTLVValueTooLarge, func() error { _, e := dg.EncodeTLVs([]dg.TLV{{Value: large}}); return e }, "%w: got %d bytes", []any{len(large)}},
		{"caller", &dg.ErrTLVDecodeLimit, func() error { _, e := dg.DecodeTLVs([]byte{1, 2}, 1); return e }, "%w: got %d bytes, limit %d", []any{2, 1}},
		{"sequence decode", &dg.ErrTLVSequenceLimit, func() error { _, e := dg.DecodeTLVs(large, 1); return e }, "%w: got %d bytes, limit %d", []any{len(large), dg.MaxTLVSequenceSize}},
		{"sequence encode", &dg.ErrTLVSequenceLimit, func() error { _, e := dg.EncodeTLVs([]dg.TLV{{Value: large[:dg.MaxTLVValueSize]}}); return e }, "%w: limit %d", []any{dg.MaxTLVSequenceSize}},
		{"count encode", &dg.ErrTLVElementLimit, func() error { _, e := dg.EncodeTLVs(make([]dg.TLV, dg.MaxTLVElements+1)); return e }, "%w: got %d, limit %d", []any{dg.MaxTLVElements + 1, dg.MaxTLVElements}},
		{"count decode", &dg.ErrTLVElementLimit, func() error {
			_, e := dg.DecodeTLVs(append(bytes.Repeat([]byte{1, 0, 0, 0}, dg.MaxTLVElements), 1), 0)
			return e
		}, "%w: limit %d", []any{dg.MaxTLVElements}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := *tc.sentinel
			defer func() { *tc.sentinel = original }()
			replacement := &tlvSentinel{}
			for _, sentinel := range []error{original, replacement, nil} {
				*tc.sentinel = sentinel
				args := append([]any{sentinel}, tc.args...)
				want := fmt.Errorf(tc.format, args...)
				got := tc.run()
				if got == nil || got.Error() != want.Error() || errors.Unwrap(got) != sentinel {
					t.Fatalf("got %v, want %v", got, want)
				}
				if sentinel != nil && !errors.Is(fmt.Errorf("outer: %w", got), sentinel) {
					t.Fatal("wrapped identity lost")
				}
				if sentinel == replacement {
					var target *tlvSentinel
					if !errors.As(got, &target) || target != replacement {
						t.Fatal("errors.As lost")
					}
				}
			}
		})
	}
}

var tlvAPISink []dg.TLV
var tlvWireSink []byte

func TestTLVAllocationBudgets(t *testing.T) {
	for _, tc := range []struct {
		wire []byte
		want float64
	}{
		{nil, 0}, {[]byte{1, 1, 0, 7}, 2}, {bytes.Repeat([]byte{1, 1, 0, 7}, 4), 5}, {bytes.Repeat([]byte{1, 0, 0, 0}, dg.MaxTLVElements), 1},
	} {
		if got := testing.AllocsPerRun(50, func() { tlvAPISink, _ = dg.DecodeTLVs(tc.wire, 0) }); got != tc.want {
			t.Fatalf("decode allocs=%g want=%g", got, tc.want)
		}
	}
	fields := []dg.TLV{{Type: 1, Value: make([]byte, 60000)}}
	if got := testing.AllocsPerRun(50, func() { tlvWireSink, _ = dg.EncodeTLVs(fields) }); got != 1 {
		t.Fatalf("encode allocs=%g", got)
	}
}
