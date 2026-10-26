package dgproto

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

// MaxErrorContextSize is the raw Error UTF-8 byte limit at zero frame padding.
// ErrReasonTooLong also applies to ErrorMessage.Context exceeding this limit.
// Subtract actual frame padding from this budget for padded frames.
const MaxErrorContextSize = MaxEncryptedPayloadSize - 2

// ErrorMessage reports a recoverable protocol condition with optional raw context.
// Wire type 0x09 is a uint16 little-endian code followed by raw UTF-8, not TLV.
// MarshalBinary returns ErrReasonTooLong when Context exceeds MaxErrorContextSize.
type ErrorMessage struct {
	// Code identifies the reported protocol condition.
	Code uint16
	// Context is optional UTF-8 diagnostic text, at most MaxErrorContextSize bytes.
	Context string
}

// MarshalBinary returns owned storage, validating UTF-8 before the byte limit.
func (m ErrorMessage) MarshalBinary() ([]byte, error) {
	if !utf8.ValidString(m.Context) {
		return nil, ErrInvalidUTF8
	}
	if len(m.Context) > MaxErrorContextSize {
		return nil, fmt.Errorf("%w: got %d", ErrReasonTooLong, len(m.Context))
	}
	buf := make([]byte, 2+len(m.Context))
	binary.LittleEndian.PutUint16(buf, m.Code)
	copy(buf[2:], m.Context)
	return buf, nil
}

// UnmarshalBinary copies raw context and leaves m unchanged on validation error.
func (m *ErrorMessage) UnmarshalBinary(data []byte) error {
	code, text, err := unmarshalErrorMessage(data)
	if err == nil {
		*m = ErrorMessage{Code: code, Context: text}
	}
	return err
}

func unmarshalErrorMessage(data []byte) (uint16, string, error) {
	// Direct current-sentinel return deliberately preserves nil-sentinel zeros.
	if len(data) < 2 {
		return 0, "", ErrMessageTooShort
	}
	if len(data) > MaxEncryptedPayloadSize {
		return 0, "", ErrMessageLength
	}
	if !utf8.Valid(data[2:]) {
		return 0, "", ErrInvalidUTF8
	}
	return binary.LittleEndian.Uint16(data[:2]), string(data[2:]), nil
}
