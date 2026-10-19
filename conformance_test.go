// conformance_test.go tests the Go implementation against normative requirement
// IDs from docs/protocol/dgproto-v1.md. Test names are prefixed with the spec
// section they exercise: WIRE = §3 framing/encoding, HS = §4 handshake/crypto,
// SESS = §4.4/§5 session and message types.
//
// Each test is a minimal, focused probe of a single matrix row requirement so
// that a failing test maps to an unambiguous spec clause rather than an
// aggregate behaviour.
package dgproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// ── WIRE-3.1: Fixed 40-byte header, DGP1 magic, version 1 ────────────────────

// TestWIRE31HeaderTruncationAtEveryByte verifies that every input shorter than
// HeaderSize (0 … HeaderSize-1 bytes) is rejected with ErrHeaderTooShort.
func TestWIRE31HeaderTruncationAtEveryByte(t *testing.T) {
	hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
	wire, err := hdr.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	for n := 0; n < HeaderSize; n++ {
		n := n
		t.Run(fmt.Sprintf("len=%d", n), func(t *testing.T) {
			var h Header
			if err := h.UnmarshalBinary(wire[:n]); !errors.Is(err, ErrHeaderTooShort) {
				t.Errorf("UnmarshalBinary(%d bytes) = %v, want %v", n, err, ErrHeaderTooShort)
			}
		})
	}
}

// TestWIRE31HeaderTrailingBytesPermitted verifies that bytes following a
// complete header are silently ignored. UnmarshalBinary reads exactly
// HeaderSize bytes; trailing content must not produce an error or corrupt the
// decoded header.
func TestWIRE31HeaderTrailingBytesPermitted(t *testing.T) {
	hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
	wire, _ := hdr.MarshalBinary()
	extra := append(bytes.Clone(wire), 0xff, 0x00, 0xab)
	var got Header
	if err := got.UnmarshalBinary(extra); err != nil {
		t.Fatalf("UnmarshalBinary with trailing bytes: %v", err)
	}
	if got != hdr {
		t.Errorf("header changed with trailing bytes:\n got  %+v\n want %+v", got, hdr)
	}
}

// TestWIRE31InvalidMagicEachByte verifies that each of the four magic bytes
// 'D', 'G', 'P', '1' at wire offsets 0-3 is individually mandatory. Mutating
// any one byte must produce ErrInvalidMagic.
func TestWIRE31InvalidMagicEachByte(t *testing.T) {
	hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
	wire, _ := hdr.MarshalBinary()
	for i := 0; i < 4; i++ {
		i := i
		t.Run(fmt.Sprintf("magic[%d]", i), func(t *testing.T) {
			b := bytes.Clone(wire)
			b[i] ^= 0xff
			var h Header
			if err := h.UnmarshalBinary(b); !errors.Is(err, ErrInvalidMagic) {
				t.Errorf("mutated magic byte %d: got %v, want %v", i, err, ErrInvalidMagic)
			}
		})
	}
}

// ── WIRE-3.2: Reserved bytes, flag bits, frame size ───────────────────────────

// TestWIRE32ReservedBytesPreservedOnReceive verifies that the four reserved
// wire octets at positions 7 and 37-39 are accepted and preserved verbatim
// when decoding an inbound header. AEAD authenticates the exact received header
// bytes, so they must not be zeroed or rejected by the receive path.
func TestWIRE32ReservedBytesPreservedOnReceive(t *testing.T) {
	hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
	wire, _ := hdr.MarshalBinary()

	cases := []struct {
		wireOffset  int
		reservedIdx int
	}{
		{7, 0}, {37, 1}, {38, 2}, {39, 3},
	}
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("wire[%d]", c.wireOffset), func(t *testing.T) {
			b := bytes.Clone(wire)
			b[c.wireOffset] = 0xde
			var got Header
			if err := got.UnmarshalBinary(b); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if err := got.ValidateReceive(); err != nil {
				t.Fatalf("ValidateReceive: %v", err)
			}
			if got.Reserved[c.reservedIdx] != 0xde {
				t.Errorf("Reserved[%d] = %#x, want 0xde (value not preserved)",
					c.reservedIdx, got.Reserved[c.reservedIdx])
			}
		})
	}
}

// TestWIRE32ReservedBytesRejectedOnSend verifies that outbound senders must
// emit zero reserved bytes. MarshalBinary must reject a header with any
// nonzero Reserved element with ErrReservedFlags.
func TestWIRE32ReservedBytesRejectedOnSend(t *testing.T) {
	for i := 0; i < 4; i++ {
		i := i
		t.Run(fmt.Sprintf("Reserved[%d]", i), func(t *testing.T) {
			hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
			hdr.Reserved[i] = 0x01
			if _, err := hdr.MarshalBinary(); !errors.Is(err, ErrReservedFlags) {
				t.Errorf("MarshalBinary with Reserved[%d]=1: got %v, want %v",
					i, err, ErrReservedFlags)
			}
		})
	}
}

// TestWIRE32UnknownFlagBitsPreservedOnReceive verifies that unknown flag bits
// in a received header are preserved for forward compatibility. ValidateReceive
// must not reject flag bits that are undefined by the current MVP profile.
func TestWIRE32UnknownFlagBitsPreservedOnReceive(t *testing.T) {
	hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
	wire, _ := hdr.MarshalBinary()
	wire[5] |= 0x80 // bit 7 — not defined by any current flag constant
	var got Header
	if err := got.UnmarshalBinary(wire); err != nil {
		t.Fatalf("UnmarshalBinary with unknown flag 0x80: %v", err)
	}
	if err := got.ValidateReceive(); err != nil {
		t.Errorf("ValidateReceive rejected unknown flag bit 0x80: %v", err)
	}
	if got.Flags&0x80 == 0 {
		t.Errorf("unknown flag bit 0x80 not preserved in Flags: %#x", got.Flags)
	}
}

// TestWIRE32PostMVPFlagsRejectedOnSend verifies that MVP senders must not set
// post-MVP flag bits. MarshalBinary must reject FlagObfuscated and FlagZeroRTT
// with ErrReservedFlags.
func TestWIRE32PostMVPFlagsRejectedOnSend(t *testing.T) {
	tests := []struct {
		name string
		flag Flags
	}{
		{"FlagObfuscated", FlagObfuscated},
		{"FlagZeroRTT", FlagZeroRTT},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			hdr := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 0, 0)
			hdr.Flags |= tt.flag
			if _, err := hdr.MarshalBinary(); !errors.Is(err, ErrReservedFlags) {
				t.Errorf("MarshalBinary with %s: got %v, want %v", tt.name, err, ErrReservedFlags)
			}
		})
	}
}

// TestWIRE32FrameSizeLimitExactAndOverflow verifies that MaxFrameSize is
// exactly 65535 bytes. A frame at that exact total size must be accepted; one
// byte larger must be rejected with ErrPayloadTooLarge.
func TestWIRE32FrameSizeLimitExactAndOverflow(t *testing.T) {
	if MaxFrameSize != 65535 {
		t.Fatalf("MaxFrameSize = %d, spec requires 65535", MaxFrameSize)
	}
	tag := make([]byte, AEADTagSize)
	maxPayload := uint32(MaxFrameSize - HeaderSize - AEADTagSize)

	if _, err := NewFrame(MessageTypeEncryptedData, [16]byte{1}, 1,
		make([]byte, maxPayload), tag, nil); err != nil {
		t.Errorf("NewFrame at MaxFrameSize: %v", err)
	}

	if _, err := NewFrame(MessageTypeEncryptedData, [16]byte{1}, 1,
		make([]byte, maxPayload+1), tag, nil); !errors.Is(err, ErrPayloadTooLarge) {
		t.Errorf("NewFrame at MaxFrameSize+1: got %v, want %v", err, ErrPayloadTooLarge)
	}
}

// ── WIRE-3.3: AEAD nonce and authenticated encryption ─────────────────────────

// TestWIRE33NonceIsZeroPrefixedLE64Sequence verifies that the 12-byte AEAD
// nonce is exactly [0x00 0x00 0x00 0x00 || LE64(sequence)] for representative
// sequence values including 1, a multi-byte value, and the maximum uint64.
func TestWIRE33NonceIsZeroPrefixedLE64Sequence(t *testing.T) {
	tests := []struct {
		sequence uint64
		want     [12]byte
	}{
		{
			sequence: 1,
			want:     [12]byte{0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			// LE64(0x0102030405060708) = 08 07 06 05 04 03 02 01
			sequence: 0x0102030405060708,
			want:     [12]byte{0, 0, 0, 0, 8, 7, 6, 5, 4, 3, 2, 1},
		},
		{
			sequence: ^uint64(0), // maximum uint64: all bits set
			want:     [12]byte{0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		},
	}
	for _, tt := range tests {
		got := nonce(tt.sequence)
		if len(got) != 12 {
			t.Errorf("nonce(%#x): len = %d, want 12", tt.sequence, len(got))
			continue
		}
		if !bytes.Equal(got, tt.want[:]) {
			t.Errorf("nonce(%#x) = % x, want % x", tt.sequence, got, tt.want)
		}
		// Explicit prefix check: bytes 0-3 must be zero.
		if got[0] != 0 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
			t.Errorf("nonce(%#x) prefix bytes = % x, want 00 00 00 00", tt.sequence, got[:4])
		}
		// Explicit LE64 check: bytes 4-11 must decode to sequence.
		if seq := binary.LittleEndian.Uint64(got[4:]); seq != tt.sequence {
			t.Errorf("nonce(%#x) LE64 suffix = %d, want %d", tt.sequence, seq, tt.sequence)
		}
	}
}

// TestWIRE33AEADTagMutationFailsAuthentication verifies that flipping any bit
// in the AEAD tag causes decryption to fail with ErrAuthentication, confirming
// that the frame integrity is protected end-to-end.
func TestWIRE33AEADTagMutationFailsAuthentication(t *testing.T) {
	codec := newTestCodec(t)
	frame, err := codec.Encrypt(MessageTypeEncryptedData, [16]byte{1}, 1, []byte("auth-tag-test"), 0)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	frame.Tag[0] ^= 0xff
	if _, err := codec.Decrypt(frame); !errors.Is(err, ErrAuthentication) {
		t.Errorf("Decrypt with mutated tag: got %v, want %v", err, ErrAuthentication)
	}
}

// TestWIRE33PayloadMutationFailsAuthentication verifies that mutating any
// ciphertext byte in the payload causes decryption to fail with
// ErrAuthentication.
func TestWIRE33PayloadMutationFailsAuthentication(t *testing.T) {
	codec := newTestCodec(t)
	frame, err := codec.Encrypt(MessageTypeEncryptedData, [16]byte{1}, 1, []byte("ciphertext-mutation"), 0)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	frame.Payload[0] ^= 0xff
	if _, err := codec.Decrypt(frame); !errors.Is(err, ErrAuthentication) {
		t.Errorf("Decrypt with mutated payload: got %v, want %v", err, ErrAuthentication)
	}
}

// TestWIRE33PaddingIsAuthenticatedAAD verifies that cleartext padding is
// included in AEAD associated data (AAD = header || padding). Mutating a
// padding byte after encryption must cause decryption to fail with
// ErrAuthentication, proving the padding bytes are authenticated.
func TestWIRE33PaddingIsAuthenticatedAAD(t *testing.T) {
	codec := newTestCodec(t)
	frame, err := codec.Encrypt(MessageTypeEncryptedData, [16]byte{1}, 1, []byte("padded-frame"), 8)
	if err != nil {
		t.Fatalf("Encrypt with 8-byte padding: %v", err)
	}
	if len(frame.Padding) == 0 {
		t.Fatal("expected nonzero padding bytes in encrypted frame")
	}
	frame.Padding[0] ^= 0xff
	if _, err := codec.Decrypt(frame); !errors.Is(err, ErrAuthentication) {
		t.Errorf("Decrypt with mutated padding: got %v, want %v", err, ErrAuthentication)
	}
}

// ── HS-4.3: Encrypted data frame preconditions ────────────────────────────────

// TestHS43ZeroSequenceRejected verifies that sequence 0 is rejected before
// encryption. The spec requires sequence numbers to be nonzero for all
// encrypted data frames.
func TestHS43ZeroSequenceRejected(t *testing.T) {
	codec := newTestCodec(t)
	if _, err := codec.Encrypt(MessageTypeEncryptedData, [16]byte{1}, 0, nil, 0); !errors.Is(err, ErrInvalidSequence) {
		t.Errorf("Encrypt(seq=0): got %v, want %v", err, ErrInvalidSequence)
	}
}

// TestHS43ZeroSessionIDRejected verifies that the all-zero session ID is
// rejected before encryption. Encrypted frames must carry a nonzero session ID.
func TestHS43ZeroSessionIDRejected(t *testing.T) {
	codec := newTestCodec(t)
	if _, err := codec.Encrypt(MessageTypeEncryptedData, [16]byte{}, 1, nil, 0); !errors.Is(err, ErrInvalidSessionID) {
		t.Errorf("Encrypt(sessionID=zero): got %v, want %v", err, ErrInvalidSessionID)
	}
}

// ── SESS-5: Message type registry and MVP restrictions ────────────────────────

// TestSESS5ResumptionTicketTypeRejectedByCodec verifies that the Codec rejects
// MessageTypeResumptionTicket (0x07), which is reserved for post-MVP use and
// must not appear in MVP encrypted frames.
func TestSESS5ResumptionTicketTypeRejectedByCodec(t *testing.T) {
	codec := newTestCodec(t)
	if _, err := codec.Encrypt(MessageTypeResumptionTicket, [16]byte{1}, 1, nil, 0); !errors.Is(err, ErrUnencryptedType) {
		t.Errorf("Encrypt(0x07/ResumptionTicket): got %v, want %v", err, ErrUnencryptedType)
	}
}

// TestSESS5HandshakeTypesRejectedByCodec verifies that the Codec rejects
// handshake message types (0x01, 0x02) as session payload types. Handshake
// frames use a separate path and must not be routable as session data.
func TestSESS5HandshakeTypesRejectedByCodec(t *testing.T) {
	codec := newTestCodec(t)
	for _, mt := range []MessageType{MessageTypeHandshakeInit, MessageTypeHandshakeResponse} {
		mt := mt
		t.Run(fmt.Sprintf("type=0x%02x", mt), func(t *testing.T) {
			if _, err := codec.Encrypt(mt, [16]byte{1}, 1, nil, 0); !errors.Is(err, ErrUnencryptedType) {
				t.Errorf("Encrypt(type=0x%02x): got %v, want %v", mt, err, ErrUnencryptedType)
			}
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// newTestCodec returns a ChaCha20-Poly1305 Codec with a deterministic 32-byte
// key for use in conformance tests. The key value is arbitrary but fixed so
// that test vectors can be reproduced.
func newTestCodec(t *testing.T) *Codec {
	t.Helper()
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1) // 0x01 … 0x20
	}
	codec, err := NewCodec(CipherChaCha20Poly1305, key)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	return codec
}
