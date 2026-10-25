// Package tlv implements the bounded DGProto TLV wire codec over byte slices.
// Public types, value ownership and replaceable error sentinels belong to root.
package tlv

import (
	"encoding/binary"
	"fmt"
)

const (
	HeaderSize      = 3
	MaxValueSize    = 1<<16 - 1
	MaxSequenceSize = 1<<16 - 1
	MaxElements     = MaxSequenceSize / 4
)

// Kind identifies a failure without binding the codec to public error variables.
type Kind uint8

const (
	OK Kind = iota
	TooShort
	TruncatedValue
	TruncatedPadding
	ValueTooLarge
	DecodeLimit
	SequenceTooLarge
	SequenceFull
	TooManyElements
	ElementsFull
)

// Failure is allocation-free validation detail; a zero value means success.
// Wrap formats the original diagnostic around the caller's current sentinel.
type Failure struct {
	Kind                     Kind
	Offset, Got, Need, Limit int
}

func (f Failure) Wrap(sentinel error) error {
	switch f.Kind {
	case TooShort:
		return fmt.Errorf("%w at offset %d: got %d header bytes", sentinel, f.Offset, f.Got)
	case TruncatedValue:
		return fmt.Errorf("%w at offset %d: value needs %d bytes, got %d", sentinel, f.Offset, f.Need, f.Got)
	case TruncatedPadding:
		return fmt.Errorf("%w at offset %d: padding needs %d bytes, got %d", sentinel, f.Offset, f.Need, f.Got)
	case ValueTooLarge:
		return fmt.Errorf("%w: got %d bytes", sentinel, f.Got)
	case DecodeLimit, SequenceTooLarge:
		return fmt.Errorf("%w: got %d bytes, limit %d", sentinel, f.Got, f.Limit)
	case SequenceFull, ElementsFull:
		return fmt.Errorf("%w: limit %d", sentinel, f.Limit)
	case TooManyElements:
		return fmt.Errorf("%w: got %d, limit %d", sentinel, f.Got, f.Limit)
	default:
		return nil
	}
}

// Align4 is also used by root's existing bounded message-size helper.
func Align4(n int) int { return (n + 3) &^ 3 }

func EncodedLen(valueLen int) (int, Failure) {
	if valueLen > MaxValueSize {
		return 0, Failure{Kind: ValueTooLarge, Got: valueLen}
	}
	return Align4(HeaderSize + valueLen), Failure{}
}

func CheckCount(count int) Failure {
	if count > MaxElements {
		return Failure{Kind: TooManyElements, Got: count, Limit: MaxElements}
	}
	return Failure{}
}

// AddLen checks value size before sequence capacity, without overflowing total.
// total must be a previously accepted sequence size and valueLen a slice length.
func AddLen(total, valueLen int) (int, Kind) {
	// Return a scalar classification on this per-field hot path rather than
	// copying a diagnostic record for every successful empty field.
	if valueLen > MaxValueSize {
		return 0, ValueTooLarge
	}
	n := Align4(HeaderSize + valueLen)
	if n > MaxSequenceSize-total {
		return 0, SequenceFull
	}
	return total + n, OK
}

// Validate is the first decode pass: reject the entire sequence before any
// descriptor or value allocation. Protocol size precedes caller size, then field
// count, header, value and padding checks occur in that order.
func Validate(data []byte, maxBytes int) (int, Failure) {
	if len(data) > MaxSequenceSize {
		return 0, Failure{Kind: SequenceTooLarge, Got: len(data), Limit: MaxSequenceSize}
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return 0, Failure{Kind: DecodeLimit, Got: len(data), Limit: maxBytes}
	}
	count := 0
	for offset := 0; offset < len(data); {
		if count == MaxElements {
			return 0, Failure{Kind: ElementsFull, Limit: MaxElements}
		}
		remaining := len(data) - offset
		if remaining < HeaderSize {
			return 0, Failure{Kind: TooShort, Offset: offset, Got: remaining}
		}
		valueLen := int(binary.LittleEndian.Uint16(data[offset+1 : offset+HeaderSize]))
		if valueLen > remaining-HeaderSize {
			return 0, Failure{Kind: TruncatedValue, Offset: offset, Need: valueLen, Got: remaining - HeaderSize}
		}
		unpadded := HeaderSize + valueLen
		paddingLen := (4 - unpadded%4) % 4
		if paddingLen > remaining-unpadded {
			return 0, Failure{Kind: TruncatedPadding, Offset: offset, Need: paddingLen, Got: remaining - unpadded}
		}
		count++
		offset += unpadded + paddingLen
	}
	return count, Failure{}
}

// Read is the second decode pass, returning a borrowed value and next offset.
// Call only on unchanged Validate-accepted input at a validated field boundary.
// Root copies directly into its own concrete descriptor, avoiding an intermediate
// descriptor slice or a second value copy. Empty borrowed values have length zero.
func Read(data []byte, offset int) (typ uint8, value []byte, next int) {
	valueLen := int(binary.LittleEndian.Uint16(data[offset+1 : offset+HeaderSize]))
	end := offset + HeaderSize + valueLen
	return data[offset], data[offset+HeaderSize : end], offset + Align4(HeaderSize+valueLen)
}

// Write encodes one field into caller-owned zeroed output and returns its next
// offset. Call only after EncodedLen/AddLen validation and output allocation.
// Zeroed output supplies padding; input and output must not overlap.
func Write(data []byte, offset int, typ uint8, value []byte) int {
	data[offset] = typ
	binary.LittleEndian.PutUint16(data[offset+1:offset+HeaderSize], uint16(len(value)))
	copy(data[offset+HeaderSize:], value)
	return offset + Align4(HeaderSize+len(value))
}

func Marshal(typ uint8, value []byte) ([]byte, Failure) {
	n, failure := EncodedLen(len(value))
	if failure.Kind != OK {
		return nil, failure
	}
	data := make([]byte, n)
	Write(data, 0, typ, value)
	return data, Failure{}
}
