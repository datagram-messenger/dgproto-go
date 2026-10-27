package dgproto_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"

	dg "github.com/datagram-messenger/dgproto-go"
)

var _ func(dg.EncryptedData) (int, error) = dg.EncryptedData.EncodedLen
var _ func(dg.EncryptedData, uint8) error = dg.EncryptedData.ValidateFrameSize

func encryptedSizeShapes() map[string]dg.EncryptedData {
	shapes := map[string]dg.EncryptedData{
		"nil":          {},
		"empty":        {Fields: []dg.TLV{}},
		"max-unpadded": {Fields: []dg.TLV{{Type: 255, Value: make([]byte, 65469)}}},
		"max-pad255":   {Fields: []dg.TLV{{Type: 255, Value: make([]byte, 65217)}}},
	}
	for n := range 8 {
		shapes[fmt.Sprintf("value%d", n)] = dg.EncryptedData{
			StreamID: 0x1234, AppMessageType: 255,
			Fields: []dg.TLV{{Type: 0, Value: bytes.Repeat([]byte{0xa5}, n)}},
		}
	}
	fields := make([]dg.TLV, 256)
	for i := range fields {
		fields[i] = dg.TLV{Type: uint8(255 - i), Value: bytes.Repeat([]byte{byte(i)}, i%8)}
	}
	shapes["all-types"] = dg.EncryptedData{StreamID: 65535, AppMessageType: 255, Fields: fields}
	return shapes
}

func TestEncryptedDataEncodedLenWireAndOwnership(t *testing.T) {
	for name, m := range encryptedSizeShapes() {
		t.Run(name, func(t *testing.T) {
			// Independent expected bytes pin prefix, order, values and zero alignment.
			want := make([]byte, 4)
			binary.LittleEndian.PutUint16(want, m.StreamID)
			want[2] = m.AppMessageType
			for _, field := range m.Fields {
				want = append(want, field.Type, byte(len(field.Value)), byte(len(field.Value)>>8))
				want = append(want, field.Value...)
				for len(want)%4 != 0 {
					want = append(want, 0)
				}
			}
			saved := m
			if m.Fields != nil {
				saved.Fields = append(make([]dg.TLV, 0, len(m.Fields)), m.Fields...)
			}
			n, err := m.EncodedLen()
			if err != nil || n != len(want) || n%4 != 0 {
				t.Fatalf("EncodedLen = %d, %v; want %d", n, err, len(want))
			}
			wire, err := m.MarshalBinary()
			if err != nil || wire == nil || !bytes.Equal(wire, want) {
				t.Fatalf("MarshalBinary differs from expected bytes: %v", err)
			}
			before := append([]byte(nil), wire...)
			for _, pad := range []uint8{0, 1, 255} {
				_ = m.ValidateFrameSize(pad)
			}
			got, err := m.MarshalBinary()
			if err != nil || !bytes.Equal(got, before) || !reflect.DeepEqual(m, saved) {
				t.Fatal("preflight mutated input or empty shapes")
			}
			wire[0] ^= 0xff
			again, err := m.MarshalBinary()
			if err != nil || !bytes.Equal(again, before) {
				t.Fatal("MarshalBinary output aliases message")
			}
			for i := range m.Fields {
				if len(m.Fields[i].Value) != 0 {
					m.Fields[i].Value[0] ^= 0xff
					if !bytes.Equal(got, before) {
						t.Fatal("MarshalBinary output aliases input")
					}
					break
				}
			}
		})
	}
}

func TestEncryptedDataSizeErrorPriority(t *testing.T) {
	large := make([]byte, dg.MaxTLVValueSize+1)
	many := make([]dg.TLV, dg.MaxTLVElements+1)
	// Count cannot be reached for EncryptedData: >256 types necessarily repeat.
	// Pin the duplicate-before-count behavior rather than adding a new policy.
	many[0].Value = large
	cases := []struct {
		name     string
		fields   []dg.TLV
		sentinel error
		want     string
	}{
		{"duplicate-before-value", []dg.TLV{{Type: 7, Value: large}, {Type: 7}}, dg.ErrDuplicateMessageTLV, fmt.Sprintf("%s: 0x07", dg.ErrDuplicateMessageTLV)},
		{"duplicate-before-count", many, dg.ErrDuplicateMessageTLV, fmt.Sprintf("%s: 0x00", dg.ErrDuplicateMessageTLV)},
		{"value", []dg.TLV{{Type: 1, Value: large}}, dg.ErrTLVValueTooLarge, fmt.Sprintf("%s: got %d bytes", dg.ErrTLVValueTooLarge, len(large))},
		{"single-sequence", []dg.TLV{{Type: 1, Value: large[:65535]}}, dg.ErrTLVSequenceLimit, fmt.Sprintf("%s: limit %d", dg.ErrTLVSequenceLimit, dg.MaxTLVSequenceSize)},
		{"aggregate-sequence", []dg.TLV{{Type: 1, Value: large[:32765]}, {Type: 2, Value: large[:32765]}}, dg.ErrTLVSequenceLimit, fmt.Sprintf("%s: limit %d", dg.ErrTLVSequenceLimit, dg.MaxTLVSequenceSize)},
		{"envelope", []dg.TLV{{Type: 1, Value: large[:65470]}}, dg.ErrMessageLength, fmt.Sprintf("%s: got 65480, limit %d", dg.ErrMessageLength, dg.MaxEncryptedPayloadSize)},
		{"envelope-before-padding", []dg.TLV{{Type: 1, Value: large[:65529]}}, dg.ErrMessageLength, fmt.Sprintf("%s: got 65536, limit %d", dg.ErrMessageLength, dg.MaxEncryptedPayloadSize)},
		{"later-value-before-envelope", []dg.TLV{{Type: 1, Value: large[:65470]}, {Type: 2, Value: large}}, dg.ErrTLVValueTooLarge, fmt.Sprintf("%s: got %d bytes", dg.ErrTLVValueTooLarge, len(large))},
		{"earlier-sequence-before-later-value", []dg.TLV{{Type: 1, Value: large[:65535]}, {Type: 2, Value: large}}, dg.ErrTLVSequenceLimit, fmt.Sprintf("%s: limit %d", dg.ErrTLVSequenceLimit, dg.MaxTLVSequenceSize)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := dg.EncryptedData{StreamID: 42, AppMessageType: 99, Fields: tc.fields}
			savedFields := append([]dg.TLV(nil), m.Fields...)
			savedValues := make([][]byte, len(m.Fields))
			for i, f := range m.Fields {
				savedValues[i] = append([]byte(nil), f.Value...)
			}
			n, err := m.EncodedLen()
			if n != 0 || !errors.Is(err, tc.sentinel) || err.Error() != tc.want {
				t.Fatalf("EncodedLen = %d, %v; want %s", n, err, tc.want)
			}
			wire, marshalErr := m.MarshalBinary()
			if wire != nil || marshalErr == nil || marshalErr.Error() != tc.want || !errors.Is(marshalErr, tc.sentinel) {
				t.Fatalf("MarshalBinary error changed: %v", marshalErr)
			}
			for _, pad := range []uint8{0, 1, 255} {
				err := m.ValidateFrameSize(pad)
				if !errors.Is(err, tc.sentinel) || err.Error() != tc.want {
					t.Fatalf("ValidateFrameSize(%d) = %v", pad, err)
				}
			}
			if !reflect.DeepEqual(m.Fields, savedFields) || m.StreamID != 42 || m.AppMessageType != 99 {
				t.Fatal("validation mutated descriptors")
			}
			for i, f := range m.Fields {
				if !bytes.Equal(f.Value, savedValues[i]) {
					t.Fatal("validation mutated values")
				}
			}
		})
	}
}

func TestEncryptedDataValidateFrameSizeCryptoBoundaries(t *testing.T) {
	codec, err := dg.NewCodec(dg.CipherChaCha20Poly1305, make([]byte, dg.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	var sequence uint64
	for _, pad := range []uint8{0, 1, 2, 3, 4, 254, 255} {
		// Payload lengths are multiples of four. Exact total-frame equality is
		// possible at pad3 and pad255, not pad0 or pad1.
		maxPayload := (dg.MaxEncryptedPayloadSize - int(pad)) &^ 3
		maxValue := maxPayload - 4 - dg.TLVHeaderSize
		for _, valueLen := range []int{0, maxValue - 3, maxValue, maxValue + 1, maxValue + 4} {
			m := dg.EncryptedData{Fields: []dg.TLV{{Type: 255, Value: make([]byte, valueLen)}}}
			n, sizeErr := m.EncodedLen()
			wire, marshalErr := m.MarshalBinary()
			preflightErr := m.ValidateFrameSize(pad)
			if sizeErr != nil {
				if n != 0 || marshalErr == nil || preflightErr == nil || sizeErr.Error() != marshalErr.Error() || sizeErr.Error() != preflightErr.Error() {
					t.Fatal("payload validation priority differs")
				}
				continue
			}
			if marshalErr != nil || n != len(wire) {
				t.Fatal("length differs from marshal")
			}
			sequence++ // Never reuse this synthetic test key/nonce pair.
			frame, encryptErr := codec.Encrypt(dg.MessageTypeEncryptedData, [16]byte{1}, sequence, wire, pad)
			total := dg.HeaderSize + n + dg.AEADTagSize + int(pad)
			if total > dg.MaxFrameSize {
				want := fmt.Sprintf("%s: %d bytes", dg.ErrPayloadTooLarge, n)
				if !errors.Is(preflightErr, dg.ErrPayloadTooLarge) || !errors.Is(encryptErr, dg.ErrPayloadTooLarge) || preflightErr.Error() != want || encryptErr.Error() != want {
					t.Fatalf("pad%d value%d: preflight=%v encrypt=%v", pad, valueLen, preflightErr, encryptErr)
				}
				continue
			}
			if preflightErr != nil || encryptErr != nil {
				t.Fatalf("pad%d value%d: preflight=%v encrypt=%v", pad, valueLen, preflightErr, encryptErr)
			}
			frameWire, err := frame.MarshalBinary()
			if err != nil || len(frameWire) != total || frame.Header.FrameSize() != uint64(total) || len(frame.Padding) != int(pad) {
				t.Fatalf("complete frame size differs: %v", err)
			}
			plain, err := codec.Decrypt(frame)
			if err != nil || !bytes.Equal(plain, wire) {
				t.Fatalf("crypto round trip differs: %v", err)
			}
			if valueLen == maxValue && total != dg.MaxFrameSize-((dg.MaxEncryptedPayloadSize-int(pad))%4) {
				t.Fatal("incorrect aligned boundary")
			}
		}
	}
}

// Serial, like the other public sentinel tests: public variables are replaceable.
func TestEncryptedDataSizeCurrentSentinels(t *testing.T) {
	large := make([]byte, 65536)
	cases := []struct {
		sentinel *error
		m        dg.EncryptedData
		pad      uint8
		format   string
		args     []any
	}{
		{&dg.ErrDuplicateMessageTLV, dg.EncryptedData{Fields: []dg.TLV{{Type: 5}, {Type: 5}}}, 255, "%w: 0x%02x", []any{5}},
		{&dg.ErrTLVValueTooLarge, dg.EncryptedData{Fields: []dg.TLV{{Value: large}}}, 255, "%w: got %d bytes", []any{65536}},
		{&dg.ErrTLVSequenceLimit, dg.EncryptedData{Fields: []dg.TLV{{Value: large[:65535]}}}, 255, "%w: limit %d", []any{dg.MaxTLVSequenceSize}},
		{&dg.ErrMessageLength, dg.EncryptedData{Fields: []dg.TLV{{Value: large[:65470]}}}, 255, "%w: got %d, limit %d", []any{65480, dg.MaxEncryptedPayloadSize}},
		{&dg.ErrPayloadTooLarge, dg.EncryptedData{Fields: []dg.TLV{{Value: large[:65469]}}}, 255, "%w: %d bytes", []any{65476}},
	}
	for _, tc := range cases {
		func() {
			original := *tc.sentinel
			defer func() { *tc.sentinel = original }()
			for _, replacement := range []error{&tlvSentinel{}, nil} {
				*tc.sentinel = replacement
				want := fmt.Errorf(tc.format, append([]any{replacement}, tc.args...)...)
				check := func(err error) {
					t.Helper()
					if err == nil || err.Error() != want.Error() || errors.Unwrap(err) != replacement {
						t.Fatalf("current sentinel/diagnostic differs: %v; want %v", err, want)
					}
					if replacement != nil {
						var typed *tlvSentinel
						if !errors.Is(err, replacement) || !errors.As(err, &typed) {
							t.Fatal("current sentinel identity lost")
						}
					}
				}
				check(tc.m.ValidateFrameSize(tc.pad))
				if tc.sentinel != &dg.ErrPayloadTooLarge {
					n, err := tc.m.EncodedLen()
					if n != 0 {
						t.Fatal("nonzero error length")
					}
					check(err)
					_, err = tc.m.MarshalBinary()
					check(err)
				}
			}
		}()
	}
}

func TestEncryptedDataSizeZeroAllocations(t *testing.T) {
	for name, m := range encryptedSizeShapes() {
		t.Run(name, func(t *testing.T) {
			if allocs := testing.AllocsPerRun(1000, func() {
				if _, err := m.EncodedLen(); err != nil {
					panic(err)
				}
			}); allocs != 0 {
				t.Fatalf("EncodedLen allocations = %v", allocs)
			}
			for _, pad := range []uint8{0, 1, 255} {
				if m.ValidateFrameSize(pad) != nil {
					continue
				}
				if allocs := testing.AllocsPerRun(1000, func() {
					if err := m.ValidateFrameSize(pad); err != nil {
						panic(err)
					}
				}); allocs != 0 {
					t.Fatalf("ValidateFrameSize(%d) allocations = %v", pad, allocs)
				}
			}
		})
	}
}
