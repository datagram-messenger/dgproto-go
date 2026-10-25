package dgproto

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"io"

	"github.com/flynn/noise"
)

const noiseKeySize = 32

// ErrInvalidStaticKey indicates a missing or internally inconsistent key pair.
var ErrInvalidStaticKey = errors.New("dgproto: invalid static key")

// StaticKey is a Noise X25519 static identity.
type StaticKey struct {
	private [noiseKeySize]byte
	public  [noiseKeySize]byte
}

// GenerateStaticKey creates a static identity using cryptographic randomness.
func GenerateStaticKey() (StaticKey, error) { return generateStaticKey(rand.Reader) }

func generateStaticKey(random io.Reader) (StaticKey, error) {
	pair, err := noise.DH25519.GenerateKeypair(random)
	if err != nil {
		return StaticKey{}, ErrHandshake
	}
	return staticKeyFromPair(pair)
}

// LoadStaticKey loads a 32-byte X25519 private key and derives its public key.
func LoadStaticKey(private []byte) (StaticKey, error) {
	if len(private) != noiseKeySize {
		return StaticKey{}, ErrHandshake
	}
	pair, err := noise.DH25519.GenerateKeypair(&fixedReader{data: append([]byte(nil), private...)})
	if err != nil {
		return StaticKey{}, ErrHandshake
	}
	return staticKeyFromPair(pair)
}

func staticKeyFromPair(pair noise.DHKey) (StaticKey, error) {
	if len(pair.Private) != noiseKeySize || len(pair.Public) != noiseKeySize {
		return StaticKey{}, ErrHandshake
	}
	var key StaticKey
	copy(key.private[:], pair.Private)
	copy(key.public[:], pair.Public)
	return key, nil
}

// Public returns an owned copy of the 32-byte X25519 static public key.
// The returned slice is independent of the StaticKey and may be shared freely.
func (k StaticKey) Public() []byte { return append([]byte(nil), k.public[:]...) }

func (k StaticKey) validate() error {
	if k.private == ([noiseKeySize]byte{}) || k.public == ([noiseKeySize]byte{}) {
		return ErrInvalidStaticKey
	}
	derived, err := LoadStaticKey(k.private[:])
	if err != nil || subtle.ConstantTimeCompare(derived.public[:], k.public[:]) != 1 {
		return ErrInvalidStaticKey
	}
	return nil
}

func (k StaticKey) noiseKey() noise.DHKey {
	return noise.DHKey{Private: append([]byte(nil), k.private[:]...), Public: append([]byte(nil), k.public[:]...)}
}

type fixedReader struct{ data []byte }

func (r *fixedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
