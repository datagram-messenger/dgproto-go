package dgproto

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

const textTLVType = 1

// EncryptedData is the application envelope carried inside an encrypted frame.
type EncryptedData struct {
	// StreamID identifies the application stream.
	StreamID uint16
	// AppMessageType identifies the application-defined message kind.
	AppMessageType uint8
	// Fields contains uniquely typed application TLVs.
	Fields []TLV
}

// EncodedLen returns the marshaled payload length: the 4-byte envelope plus
// each TLV's header, value and alignment padding. It applies MarshalBinary's
// validation in the same order and returns zero on error. Valid calls allocate
// no storage. The frame header, AEAD tag and random frame padding are excluded;
// use ValidateFrameSize to check the complete encrypted frame budget.
//
// This is not a snapshot or reservation. Keep m and its reachable buffers
// unchanged during the call and through subsequent serialization or queued use.
func (m EncryptedData) EncodedLen() (int, error) {
	if err := rejectDuplicateTLVs(m.Fields); err != nil {
		return 0, err
	}
	fieldsLen, err := encodedTLVsLen(m.Fields)
	if err != nil {
		return 0, err
	}
	if fieldsLen > MaxEncryptedPayloadSize-4 {
		return 0, fmt.Errorf("%w: got %d, limit %d", ErrMessageLength, 4+fieldsLen, MaxEncryptedPayloadSize)
	}
	return 4 + fieldsLen, nil
}

// ValidateFrameSize checks that m can fit in a complete encrypted frame with
// exactly padLength random padding bytes, including HeaderSize and AEADTagSize.
// It first applies EncodedLen's validation, then wraps ErrPayloadTooLarge if
// the requested padding exceeds the remaining frame budget, as Codec.Encrypt
// does. Valid calls allocate no storage.
//
// This optional preflight neither snapshots inputs nor reserves queue capacity
// or session state, and does not guarantee a successful send or delivery. Keep
// inputs unchanged as required by EncodedLen and the sending API's ownership
// contract. Sending APIs do not automatically perform this preflight.
func (m EncryptedData) ValidateFrameSize(padLength uint8) error {
	n, err := m.EncodedLen()
	if err != nil {
		return err
	}
	if HeaderSize+n+AEADTagSize+int(padLength) > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes", ErrPayloadTooLarge, n)
	}
	return nil
}

// MarshalBinary encodes m, rejecting duplicate field types, and returns owned storage.
func (m EncryptedData) MarshalBinary() ([]byte, error) {
	if err := rejectDuplicateTLVs(m.Fields); err != nil {
		return nil, err
	}
	fields, err := EncodeTLVs(m.Fields)
	if err != nil {
		return nil, err
	}
	if len(fields) > MaxEncryptedPayloadSize-4 {
		return nil, fmt.Errorf("%w: got %d, limit %d", ErrMessageLength, 4+len(fields), MaxEncryptedPayloadSize)
	}
	buf := make([]byte, 4, 4+len(fields))
	binary.LittleEndian.PutUint16(buf[:2], m.StreamID)
	buf[2] = m.AppMessageType
	return append(buf, fields...), nil
}

// UnmarshalBinary decodes data into m and copies all TLV values.
func (m *EncryptedData) UnmarshalBinary(data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("%w: got %d, want at least 4", ErrMessageTooShort, len(data))
	}
	if data[3] != 0 {
		return ErrMessageReserved
	}
	fields, err := DecodeTLVs(data[4:], MaxEncryptedPayloadSize-4)
	if err != nil {
		return err
	}
	if err := rejectDuplicateTLVs(fields); err != nil {
		return err
	}
	*m = EncryptedData{StreamID: binary.LittleEndian.Uint16(data[:2]), AppMessageType: data[2], Fields: fields}
	return nil
}

func rejectDuplicateTLVs(fields []TLV) error {
	var seen [256]bool
	for _, field := range fields {
		if seen[field.Type] {
			return fmt.Errorf("%w: 0x%02x", ErrDuplicateMessageTLV, field.Type)
		}
		seen[field.Type] = true
	}
	return nil
}

// SessionClose requests graceful termination with an optional UTF-8 reason.
type SessionClose struct {
	// Code is an MVP close code in the range zero through three.
	Code uint16
	// Reason is optional UTF-8 close text.
	Reason string
}

// MarshalBinary encodes the close code and optional UTF-8 reason.
func (m SessionClose) MarshalBinary() ([]byte, error) {
	if m.Code > 3 {
		return nil, fmt.Errorf("%w: 0x%04x", ErrInvalidCloseCode, m.Code)
	}
	return marshalTextMessage(m.Code, m.Reason)
}

// UnmarshalBinary decodes a session-close payload into m.
func (m *SessionClose) UnmarshalBinary(data []byte) error {
	code, text, err := unmarshalTextMessage(data)
	if err != nil {
		return err
	}
	if code > 3 {
		return fmt.Errorf("%w: 0x%04x", ErrInvalidCloseCode, code)
	}
	*m = SessionClose{Code: code, Reason: text}
	return nil
}

func marshalTextMessage(code uint16, text string) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUTF8
	}
	if len(text) > MaxReasonSize {
		return nil, fmt.Errorf("%w: got %d", ErrReasonTooLong, len(text))
	}
	buf := make([]byte, 2)
	binary.LittleEndian.PutUint16(buf, code)
	if text == "" {
		return buf, nil
	}
	field, _ := NewTLV(textTLVType, []byte(text))
	encoded, err := field.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return append(buf, encoded...), nil
}

func unmarshalTextMessage(data []byte) (uint16, string, error) {
	if len(data) < 2 {
		return 0, "", ErrMessageTooShort
	}
	code := binary.LittleEndian.Uint16(data[:2])
	if len(data) == 2 {
		return code, "", nil
	}
	fields, err := DecodeTLVs(data[2:], align4(TLVHeaderSize+MaxReasonSize))
	if err != nil {
		return 0, "", err
	}
	if err := rejectDuplicateTLVs(fields); err != nil {
		return 0, "", err
	}
	if len(fields) != 1 {
		return 0, "", ErrUnknownMessageTLV
	}
	if fields[0].Type != textTLVType {
		return 0, "", fmt.Errorf("%w: 0x%02x", ErrUnknownMessageTLV, fields[0].Type)
	}
	if !utf8.Valid(fields[0].Value) {
		return 0, "", ErrInvalidUTF8
	}
	return code, string(fields[0].Value), nil
}
