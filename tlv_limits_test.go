package dgproto

import (
	"errors"
	"testing"
)

// This maximum-value probe intentionally uses the private facade alignment.
func TestTLVLimitsAndMaximumValue(t *testing.T) {
	maxWireValueSize := MaxTLVSequenceSize - TLVHeaderSize
	for align4(TLVHeaderSize+maxWireValueSize) > MaxTLVSequenceSize {
		maxWireValueSize--
	}
	max := TLV{Type: 1, Value: make([]byte, maxWireValueSize)}
	wire, err := max.MarshalBinary()
	if err != nil {
		t.Fatalf("maximum sequence value: %v", err)
	}
	if _, err := DecodeTLVs(wire, len(wire)); err != nil {
		t.Fatalf("decode maximum sequence value: %v", err)
	}
	if _, err := DecodeTLVs(wire, len(wire)-1); !errors.Is(err, ErrTLVDecodeLimit) {
		t.Fatalf("limit error = %v, want ErrTLVDecodeLimit", err)
	}
	maxLengthField := TLV{Type: 1, Value: make([]byte, MaxTLVValueSize)}
	maxLengthWire, err := maxLengthField.MarshalBinary()
	if err != nil {
		t.Fatalf("maximum uint16 value: %v", err)
	}
	if _, err := DecodeTLVs(maxLengthWire, 0); !errors.Is(err, ErrTLVSequenceLimit) {
		t.Fatalf("maximum uint16 sequence error = %v, want ErrTLVSequenceLimit", err)
	}
	tooLarge := TLV{Type: 1, Value: make([]byte, MaxTLVValueSize+1)}
	if _, err := tooLarge.MarshalBinary(); !errors.Is(err, ErrTLVValueTooLarge) {
		t.Fatalf("oversize error = %v, want ErrTLVValueTooLarge", err)
	}
	if _, err := NewTLV(1, tooLarge.Value); !errors.Is(err, ErrTLVValueTooLarge) {
		t.Fatalf("NewTLV oversize error = %v, want ErrTLVValueTooLarge", err)
	}
}
