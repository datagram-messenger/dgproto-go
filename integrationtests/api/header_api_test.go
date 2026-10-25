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

var _ encoding.BinaryMarshaler = dg.Header{}
var _ encoding.BinaryUnmarshaler = (*dg.Header)(nil)
var headerBytesSink []byte
var headerValueSink dg.Header

func TestHeaderPublicIdentityAndOwnership(t *testing.T) {
	typ := reflect.TypeOf(dg.Header{})
	if typ.PkgPath() != "github.com/datagram-messenger/dgproto-go" || typ.Name() != "Header" {
		t.Fatal(typ)
	}
	names := []string{"Version", "Flags", "MessageType", "Reserved", "SessionID", "Sequence", "PayloadLength", "PadLength"}
	types := []reflect.Type{reflect.TypeOf(uint8(0)), reflect.TypeOf(dg.Flags(0)), reflect.TypeOf(dg.MessageType(0)), reflect.TypeOf([4]byte{}), reflect.TypeOf([16]byte{}), reflect.TypeOf(uint64(0)), reflect.TypeOf(uint32(0)), reflect.TypeOf(uint8(0))}
	if typ.NumField() != len(names) {
		t.Fatal(typ)
	}
	for i, name := range names {
		f := typ.Field(i)
		if f.Name != name || f.Type != types[i] || f.Anonymous || f.Tag != "" {
			t.Fatal(f)
		}
	}
	h := dg.NewHeader(dg.MessageTypeEncryptedData, [16]byte{1, 2, 3}, ^uint64(0), 17, 255)
	a, err := h.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 40 || cap(a) != 40 {
		t.Fatalf("shape %d/%d", len(a), cap(a))
	}
	var decoded dg.Header
	if err := decoded.UnmarshalBinary(append(a, 99)); err != nil || decoded != h {
		t.Fatalf("decode: %+v %v", decoded, err)
	}
	a[8] ^= 255
	if decoded != h || !bytes.Equal(b[8:24], h.SessionID[:]) {
		t.Fatal("alias")
	}
}

func TestHeaderCurrentMagicAndSentinels(t *testing.T) {
	magic := dg.Magic
	sentinels := []*error{&dg.ErrHeaderTooShort, &dg.ErrInvalidMagic, &dg.ErrUnsupportedVersion, &dg.ErrPaddingFlag, &dg.ErrFrameTooLarge, &dg.ErrReservedFlags}
	originals := make([]error, len(sentinels))
	for i, p := range sentinels {
		originals[i] = *p
	}
	defer func() {
		dg.Magic = magic
		for i, p := range sentinels {
			*p = originals[i]
		}
	}()
	h := dg.NewHeader(dg.MessageTypeEncryptedData, [16]byte{}, 0, 0, 0)
	dg.Magic = [4]byte{0, 255, 9, 8}
	wire, err := h.MarshalBinary()
	if err != nil || [4]byte(wire[:4]) != dg.Magic {
		t.Fatalf("magic: %x %v", wire, err)
	}
	var dst dg.Header
	if err := dst.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		index  int
		call   func() error
		suffix string
	}{
		{0, func() error { return dst.UnmarshalBinary(nil) }, ": got 0 bytes, want 40"},
		{1, func() error { b := bytes.Clone(wire); b[0] ^= 1; b[4] = 99; return dst.UnmarshalBinary(b) }, ""},
		{2, func() error { x := h; x.Version = 99; x.Flags = 255; return x.Validate() }, ": got 99, want 1"},
		{3, func() error { x := h; x.PadLength = 1; x.PayloadLength = ^uint32(0); return x.Validate() }, ""},
		{4, func() error { x := h; x.PayloadLength = ^uint32(0); x.Reserved[0] = 1; return x.Validate() }, ": 4294967351 bytes"},
		{5, func() error { x := h; x.Flags = 128; x.Reserved[0] = 1; return x.Validate() }, ": 0x80"},
		{5, func() error { x := h; x.Reserved[0] = 1; return x.Validate() }, ": reserved header bytes are nonzero"},
	}
	for _, c := range cases {
		replacement := errors.New("replacement")
		*sentinels[c.index] = replacement
		if err := c.call(); !errors.Is(err, replacement) || err.Error() != replacement.Error()+c.suffix {
			t.Fatalf("sentinel %d: %v", c.index, err)
		}
		*sentinels[c.index] = nil
		err := c.call()
		if c.suffix == "" {
			if err != nil {
				t.Fatalf("direct nil sentinel %d: %v", c.index, err)
			}
		} else if err == nil || err.Error() != fmt.Errorf("%w", error(nil)).Error()+c.suffix {
			t.Fatalf("wrapped nil sentinel %d: %v", c.index, err)
		}
		*sentinels[c.index] = originals[c.index]
	}
}

func TestHeaderNilReceiverAndAllocationParity(t *testing.T) {
	h := dg.NewHeader(dg.MessageTypeEncryptedData, [16]byte{}, 1, 0, 0)
	wire, _ := h.MarshalBinary()
	var p *dg.Header
	if err := p.UnmarshalBinary(nil); !errors.Is(err, dg.ErrHeaderTooShort) {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("valid decode into nil must panic")
			}
		}()
		_ = p.UnmarshalBinary(wire)
	}()
	if got := testing.AllocsPerRun(1000, func() {
		var x dg.Header
		if err := x.UnmarshalBinary(wire); err != nil {
			panic(err)
		}
		headerValueSink = x
	}); got != 0 {
		t.Fatalf("decode allocations %g", got)
	}
	if got := testing.AllocsPerRun(1000, func() {
		var err error
		headerBytesSink, err = h.MarshalBinary()
		if err != nil {
			panic(err)
		}
	}); got != 1 {
		t.Fatalf("encode allocations %g", got)
	}
}

func BenchmarkHeaderBoundary(b *testing.B) {
	h := dg.NewHeader(dg.MessageTypeEncryptedData, [16]byte{1, 2, 3}, ^uint64(0), 1024, 7)
	wire, _ := h.MarshalBinary()
	b.Run("encode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var err error
			headerBytesSink, err = h.MarshalBinary()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("decode", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if err := headerValueSink.UnmarshalBinary(wire); err != nil {
				b.Fatal(err)
			}
		}
	})
}
