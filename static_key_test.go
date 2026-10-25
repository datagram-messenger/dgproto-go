package dgproto

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/flynn/noise"
)

func TestStaticKeyIdentityAndOwnership(t *testing.T) {
	typ := reflect.TypeOf(StaticKey{})
	if typ.Name() != "StaticKey" || typ.PkgPath() != "github.com/datagram-messenger/dgproto-go" || !typ.Comparable() || typ.Size() != 64 {
		t.Fatalf("public concrete identity/storage changed: %v", typ)
	}
	private := bytes.Repeat([]byte{7}, noiseKeySize)
	key, err := LoadStaticKey(private)
	if err != nil {
		t.Fatal(err)
	}
	original := key
	private[0] ^= 1
	public := key.Public()
	public[0] ^= 1
	pair := key.noiseKey()
	pair.Private[0] ^= 1
	pair.Public[0] ^= 1
	if key != original || !bytes.Equal(key.Public(), original.public[:]) || key.validate() != nil {
		t.Fatal("input, public output or Noise pair aliases static identity")
	}
	loaded, err := staticKeyFromPair(noise.DHKey{Private: original.private[:], Public: original.public[:]})
	if err != nil {
		t.Fatal(err)
	}
	original.private[0] ^= 1
	original.public[0] ^= 1
	if loaded != key {
		t.Fatal("pair conversion did not copy identity")
	}
}

func TestStaticKeyCurrentSentinels(t *testing.T) {
	oldHandshake, oldStatic := ErrHandshake, ErrInvalidStaticKey
	t.Cleanup(func() { ErrHandshake, ErrInvalidStaticKey = oldHandshake, oldStatic })
	for _, replacement := range []error{errors.New("replacement identity error"), nil} {
		ErrHandshake, ErrInvalidStaticKey = replacement, replacement
		if _, err := LoadStaticKey(make([]byte, noiseKeySize-1)); err != replacement {
			t.Fatalf("load sentinel: %v", err)
		}
		if _, err := generateStaticKey(bytes.NewReader(nil)); err != replacement {
			t.Fatalf("generation sentinel: %v", err)
		}
		if _, err := staticKeyFromPair(noise.DHKey{}); err != replacement {
			t.Fatalf("pair sentinel: %v", err)
		}
		if err := (StaticKey{}).validate(); err != replacement {
			t.Fatalf("validation sentinel: %v", err)
		}
	}
}
