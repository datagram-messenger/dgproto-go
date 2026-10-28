package echoapp

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestProvisionedIdentity(t *testing.T) {
	private := bytes.Repeat([]byte{7}, 32) // Synthetic test-only key material.
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path, private, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := dgproto.LoadStaticKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key.Public(), want.Public()) {
		t.Fatal("identity changed")
	}
	stored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(stored, private) {
		t.Fatal("loader changed source file")
	}
	publicPath := filepath.Join(t.TempDir(), "pin")
	if err := os.WriteFile(publicPath, want.Public(), 0600); err != nil {
		t.Fatal(err)
	}
	pin, err := LoadPublic(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	pin[0] ^= 1
	again, err := LoadPublic(publicPath)
	if err != nil || !bytes.Equal(again, want.Public()) {
		t.Fatal("pin does not own its bytes")
	}
}

func TestInvalidIdentityFiles(t *testing.T) {
	for _, size := range []int{0, 1, 31, 33, 1 << 20} {
		t.Run(string(rune('A'+size%26)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "invalid")
			if err := os.WriteFile(path, bytes.Repeat([]byte{9}, size), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadIdentity(path); !errors.Is(err, ErrIdentityInput) {
				t.Fatalf("identity error = %v", err)
			}
			if _, err := LoadPublic(path); !errors.Is(err, ErrIdentityInput) {
				t.Fatalf("pin error = %v", err)
			}
		})
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		if _, err := LoadPublic(path); !errors.Is(err, ErrIdentityInput) || err.Error() != ErrIdentityInput.Error() {
			t.Fatalf("unsanitized error = %v", err)
		}
	}
	zero := filepath.Join(t.TempDir(), "zero")
	if err := os.WriteFile(zero, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(zero); !errors.Is(err, ErrIdentityInput) {
		t.Fatal("zero identity accepted")
	}
	if _, err := LoadPublic(zero); !errors.Is(err, ErrIdentityInput) {
		t.Fatal("zero pin accepted")
	}
}
