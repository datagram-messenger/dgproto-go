package dgproto_test

import (
	"bytes"
	"errors"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestCodecRoundTrip(t *testing.T) {
	codec, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, bytes.Repeat([]byte{0x42}, dgproto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("authenticated DGProto v1 payload")
	frame, err := codec.Encrypt(dgproto.MessageTypeEncryptedData, [16]byte{1}, 0x0102030405060708, plaintext, 17)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Payload) != len(plaintext) || len(frame.Padding) != 17 {
		t.Fatalf("body lengths = %d, %d", len(frame.Payload), len(frame.Padding))
	}
	got, err := codec.Decrypt(frame)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext = %q, want %q", got, plaintext)
	}
}

func TestNewCodecErrors(t *testing.T) {
	for _, size := range []int{0, dgproto.KeySize - 1, dgproto.KeySize + 1} {
		if _, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, make([]byte, size)); !errors.Is(err, dgproto.ErrInvalidKeySize) {
			t.Fatalf("key size %d error = %v", size, err)
		}
	}
	for _, suite := range []dgproto.CipherSuite{dgproto.CipherAES256GCM, 99} {
		if _, err := dgproto.NewCodec(suite, make([]byte, dgproto.KeySize)); !errors.Is(err, dgproto.ErrUnsupportedCipher) {
			t.Fatalf("suite %d error = %v", suite, err)
		}
	}
}

func TestCodecRejectsInvalidEncryptedHeader(t *testing.T) {
	codec, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, make([]byte, dgproto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		msgType   dgproto.MessageType
		sessionID [16]byte
		sequence  uint64
		want      error
	}{
		{"handshake type", dgproto.MessageTypeHandshakeInit, [16]byte{1}, 1, dgproto.ErrUnencryptedType},
		{"zero session", dgproto.MessageTypeEncryptedData, [16]byte{}, 1, dgproto.ErrInvalidSessionID},
		{"zero sequence", dgproto.MessageTypeEncryptedData, [16]byte{1}, 0, dgproto.ErrInvalidSequence},
		{"reserved resumption type", dgproto.MessageTypeResumptionTicket, [16]byte{1}, 1, dgproto.ErrUnencryptedType},
		{"unknown type", 0xff, [16]byte{1}, 1, dgproto.ErrUnencryptedType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := codec.Encrypt(tt.msgType, tt.sessionID, tt.sequence, nil, 0); !errors.Is(err, tt.want) {
				t.Fatalf("Encrypt() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCodecAuthenticationFailuresAreUniform(t *testing.T) {
	codec, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, bytes.Repeat([]byte{1}, dgproto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := codec.Encrypt(dgproto.MessageTypeAck, [16]byte{1}, 7, []byte("secret"), 0)
	if err != nil {
		t.Fatal(err)
	}

	mutations := []func(*dgproto.Frame){
		func(f *dgproto.Frame) { f.Payload[0] ^= 1 },
		func(f *dgproto.Frame) { f.Tag[0] ^= 1 },
		func(f *dgproto.Frame) { f.Header.Sequence++ },
		func(f *dgproto.Frame) { f.Header.SessionID[0]++ },
		func(f *dgproto.Frame) { f.Header.MessageType = dgproto.MessageTypePingPong },
	}
	for i, mutate := range mutations {
		got := frame
		got.Payload = append([]byte(nil), frame.Payload...)
		mutate(&got)
		if _, err := codec.Decrypt(got); err != dgproto.ErrAuthentication {
			t.Fatalf("mutation %d error = %v, want exact ErrAuthentication", i, err)
		}
	}
}

func TestCodecWrongKeyFailsAuthentication(t *testing.T) {
	codec, _ := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, bytes.Repeat([]byte{1}, dgproto.KeySize))
	frame, err := codec.Encrypt(dgproto.MessageTypeEncryptedData, [16]byte{1}, 1, []byte("secret"), 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, bytes.Repeat([]byte{2}, dgproto.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(frame); err != dgproto.ErrAuthentication {
		t.Fatalf("Decrypt() error = %v", err)
	}
}

func TestCodecAuthenticatesPadding(t *testing.T) {
	codec, _ := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, make([]byte, dgproto.KeySize))
	frame, err := codec.Encrypt(dgproto.MessageTypeEncryptedData, [16]byte{1}, 1, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	frame.Padding[0] ^= 1
	if _, err := codec.Decrypt(frame); err != dgproto.ErrAuthentication {
		t.Fatalf("Decrypt() error = %v, want %v", err, dgproto.ErrAuthentication)
	}
}
