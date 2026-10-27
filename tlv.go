package dgproto

import (
	"errors"

	"github.com/datagram-messenger/dgproto-go/internal/tlv"
)

const (
	// TLVHeaderSize is the encoded size of a DGProto v1 TLV header.
	TLVHeaderSize = 3
	// MaxTLVValueSize is the largest value representable by the wire length.
	MaxTLVValueSize = 1<<16 - 1
	// MaxTLVSequenceSize bounds one TLV sequence to the protocol frame limit.
	MaxTLVSequenceSize = MaxFrameSize
	// MaxTLVElements is the most empty, aligned TLVs that fit in one sequence.
	MaxTLVElements = MaxTLVSequenceSize / 4
)

var (
	// ErrTLVTooShort indicates that input cannot contain a complete TLV header.
	ErrTLVTooShort = errors.New("dgproto: TLV too short")
	// ErrTLVTruncated indicates that a TLV value or its alignment padding is incomplete.
	ErrTLVTruncated = errors.New("dgproto: truncated TLV")
	// ErrTLVValueTooLarge indicates that a value exceeds the uint16 wire length.
	ErrTLVValueTooLarge = errors.New("dgproto: TLV value exceeds uint16 length")
	// ErrTLVDecodeLimit indicates that input exceeds the caller's positive decode limit.
	ErrTLVDecodeLimit = errors.New("dgproto: TLV input exceeds decode limit")
	// ErrTLVSequenceLimit indicates that an encoded sequence exceeds MaxTLVSequenceSize.
	ErrTLVSequenceLimit = errors.New("dgproto: TLV sequence exceeds size limit")
	// ErrTLVElementLimit indicates that a sequence exceeds MaxTLVElements.
	ErrTLVElementLimit = errors.New("dgproto: TLV sequence exceeds element limit")
)

// TLV is one application field. Value is owned by the TLV and does not alias
// caller input when constructed with NewTLV or decoded with DecodeTLVs.
type TLV struct {
	Type  uint8
	Value []byte
}

// NewTLV constructs a TLV and copies value.
func NewTLV(typ uint8, value []byte) (TLV, error) {
	if _, failure := tlv.EncodedLen(len(value)); failure.Kind != tlv.OK {
		return TLV{}, tlvError(failure)
	}
	return TLV{Type: typ, Value: append([]byte(nil), value...)}, nil
}

// EncodedLen returns the encoded length including zero alignment padding.
func (t TLV) EncodedLen() (int, error) {
	n, failure := tlv.EncodedLen(len(t.Value))
	return n, tlvError(failure)
}

// MarshalBinary encodes t and zero-pads it to a 4-byte boundary.
func (t TLV) MarshalBinary() ([]byte, error) {
	buf, failure := tlv.Marshal(t.Type, t.Value)
	return buf, tlvError(failure)
}

// DecodeTLVs decodes exactly one TLV sequence. Protocol-wide size and element
// limits always apply; a positive maxBytes may impose a tighter caller limit.
// Unknown types are preserved, padding bytes are skipped without inspecting
// their content, and values are copied.
//
// Alignment padding bytes (0–3 bytes after each TLV value, rounding the
// encoded length up to a 4-byte boundary) are intentionally not zero-checked.
// The normative spec (§2.4) requires parsers to ignore alignment padding.
// This is distinct from authenticated random frame padding (§3.3).
func DecodeTLVs(data []byte, maxBytes int) ([]TLV, error) {
	count, failure := tlv.Validate(data, maxBytes)
	if failure.Kind != tlv.OK {
		return nil, tlvError(failure)
	}
	out := make([]TLV, count)
	for i, offset := 0, 0; i < count; i++ {
		// Input must remain unchanged between validation and reading. Copy
		// straight into root descriptors; no intermediate slice is allocated.
		typ, value, next := tlv.Read(data, offset)
		out[i] = TLV{Type: typ, Value: append([]byte(nil), value...)}
		offset = next
	}
	return out, nil
}

// EncodeTLVs encodes fields in order. Duplicate and unknown types are retained.
func EncodeTLVs(fields []TLV) ([]byte, error) {
	total, err := encodedTLVsLen(fields)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, total)
	offset := 0
	for _, field := range fields {
		offset = tlv.Write(buf, offset, field.Type, field.Value)
	}
	return buf, nil
}

// encodedTLVsLen preserves encoder validation order: count, then each value
// and running sequence size. It neither reads value bytes nor allocates on success.
func encodedTLVsLen(fields []TLV) (int, error) {
	if failure := tlv.CheckCount(len(fields)); failure.Kind != tlv.OK {
		return 0, tlvError(failure)
	}
	total := 0
	for _, field := range fields {
		next, kind := tlv.AddLen(total, len(field.Value))
		if kind != tlv.OK {
			return 0, tlvError(tlv.Failure{Kind: kind, Got: len(field.Value), Limit: tlv.MaxSequenceSize})
		}
		total = next
	}
	return total, nil
}

func align4(n int) int { return tlv.Align4(n) }

// Resolve sentinels at the failing call, not package initialization: public
// error variables remain dynamically replaceable, including nil values.
func tlvError(failure tlv.Failure) error {
	var sentinel error
	switch failure.Kind {
	case tlv.OK:
		return nil
	case tlv.TooShort:
		sentinel = ErrTLVTooShort
	case tlv.TruncatedValue, tlv.TruncatedPadding:
		sentinel = ErrTLVTruncated
	case tlv.ValueTooLarge:
		sentinel = ErrTLVValueTooLarge
	case tlv.DecodeLimit:
		sentinel = ErrTLVDecodeLimit
	case tlv.SequenceTooLarge, tlv.SequenceFull:
		sentinel = ErrTLVSequenceLimit
	case tlv.TooManyElements, tlv.ElementsFull:
		sentinel = ErrTLVElementLimit
	}
	return failure.Wrap(sentinel)
}
