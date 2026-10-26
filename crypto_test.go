package dgproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestNonceLayout(t *testing.T) {
	got := nonce(0x0102030405060708)
	want := make([]byte, 12)
	binary.LittleEndian.PutUint64(want[4:], 0x0102030405060708)
	if !bytes.Equal(got, want) {
		t.Fatalf("nonce = %x, want %x", got, want)
	}
}

func TestCodecAADUsesExactReceivedReservedBytes(t *testing.T) {
	codec, _ := NewCodec(CipherChaCha20Poly1305, make([]byte, KeySize))
	header := NewHeader(MessageTypeEncryptedData, [16]byte{1}, 1, 1, 0)
	header.Reserved = [4]byte{0xa7, 0xb7, 0xb8, 0xb9}
	aad, err := header.marshalBinary(false)
	if err != nil {
		t.Fatal(err)
	}
	sealed := codec.aead.Seal(nil, nonce(header.Sequence), []byte("x"), aad)
	frame := Frame{Header: header, Payload: append([]byte(nil), sealed[:1]...)}
	copy(frame.Tag[:], sealed[1:])

	plaintext, err := codec.Decrypt(frame)
	if err != nil || !bytes.Equal(plaintext, []byte("x")) {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
	for i := range frame.Header.Reserved {
		tampered := frame
		tampered.Header.Reserved[i] ^= 1
		if _, err := codec.Decrypt(tampered); err != ErrAuthentication {
			t.Fatalf("reserved byte %d tamper error = %v", i, err)
		}
	}
	if _, err := frame.MarshalBinary(); !errors.Is(err, ErrReservedFlags) {
		t.Fatalf("sender accepted nonzero reserved bytes: %v", err)
	}
}

func TestRandomBytes(t *testing.T) {
	got, err := randomBytes(bytes.NewReader([]byte{0, 1, 2}), 3)
	if err != nil || !bytes.Equal(got, []byte{0, 1, 2}) {
		t.Fatalf("padding = %v, error = %v", got, err)
	}
	if _, err := randomBytes(bytes.NewReader(nil), 1); err == nil {
		t.Fatal("expected entropy error")
	}
	if _, err := randomBytes(bytes.NewReader(nil), 256); !errors.Is(err, ErrPaddingLength) {
		t.Fatalf("length error = %v", err)
	}
}
