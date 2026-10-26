package dgproto

import (
	"bytes"
	"testing"
)

func TestHeaderReceivedOctetsRemainExact(t *testing.T) {
	base, err := NewHeader(MessageTypeEncryptedData, [16]byte{1, 2, 3}, 7, 0, 0).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// Independently exercise every wire octet and value. Semantic rejection is
	// permitted, but a successful receive must reproduce the exact first 40 bytes
	// for AAD, including reserved bytes, unknown flags and unknown message types.
	for offset := 0; offset < HeaderSize; offset++ {
		for value := 0; value < 256; value++ {
			input := bytes.Clone(base)
			input[offset] = byte(value)
			original := Header{Version: 99, Sequence: 99}
			got := original
			if err := got.UnmarshalBinary(input); err != nil {
				if got != original {
					t.Fatalf("changed on error %d/%d", offset, value)
				}
				continue
			}
			encoded, err := got.marshalBinary(false)
			if err != nil || !bytes.Equal(encoded, input) {
				t.Fatalf("AAD mismatch %d/%d: %x %x %v", offset, value, encoded, input, err)
			}
			before := got
			clear(input)
			if got != before {
				t.Fatal("receive alias")
			}
		}
	}
}
