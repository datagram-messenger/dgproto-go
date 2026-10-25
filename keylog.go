//go:build dgproto_keylog

package dgproto

import "github.com/datagram-messenger/dgproto-go/internal/keylog"

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
	keylog.Write(sessionID, sendKey, recvKey)
}

func keylogWriteEpoch(sessionID [16]byte, direction string, epoch uint32, key [KeySize]byte) {
	keylog.WriteEpoch(sessionID, direction, epoch, key)
}
