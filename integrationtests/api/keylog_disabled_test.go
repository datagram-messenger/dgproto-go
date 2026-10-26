//go:build !dgproto_keylog

package dgproto_test

import (
	"errors"
	. "github.com/datagram-messenger/dgproto-go"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultBuildDoesNotWriteKeyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.log")
	t.Setenv("DGPROTO_KEYLOG", path)

	_, err := NewSession(CipherChaCha20Poly1305, HandshakeSecrets{
		SessionID:  [16]byte{1},
		SendKey:    [KeySize]byte{2},
		ReceiveKey: [KeySize]byte{3},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("key log stat error = %v, want file not to exist", err)
	}
}
