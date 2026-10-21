//go:build dgproto_keylog

package dgproto

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"
)

// keylogOnce guards one-time initialisation of the keylog writer.
var (
	keylogOnce   sync.Once
	keylogWriter *os.File
	keylogActive bool
)

func keylogOpen() {
	path := os.Getenv("DGPROTO_KEYLOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	keylogWriter = f
	keylogActive = true
}

// keylogWrite appends session key material to the file named by the
// DGPROTO_KEYLOG environment variable. It is a no-op when the variable is
// unset. The file is opened once on the first call and never closed.
//
// Output format:
//
//	DGPROTO_SESSION_KEY <session_id_hex> <direction> <epoch> <key_hex>
//
// This function is intended exclusively for local Wireshark debugging.
// Never set DGPROTO_KEYLOG in production environments.
//
// keylogWrite is safe for concurrent use.
func keylogWrite(sessionID [16]byte, sendKey, recvKey [KeySize]byte) {
	keylogWriteEpoch(sessionID, "send", 1, sendKey)
	keylogWriteEpoch(sessionID, "recv", 1, recvKey)
}

func keylogWriteEpoch(sessionID [16]byte, direction string, epoch uint32, key [KeySize]byte) {
	keylogOnce.Do(keylogOpen)
	if !keylogActive {
		return
	}
	fmt.Fprintf(keylogWriter, "DGPROTO_SESSION_KEY %s %s %d %s\n",
		hex.EncodeToString(sessionID[:]), direction, epoch, hex.EncodeToString(key[:]))
}
