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
// Output format (two lines per session):
//
//	DGPROTO_SESSION_KEY <session_id_hex> send 1 <send_key_hex>
//	DGPROTO_SESSION_KEY <session_id_hex> recv 1 <recv_key_hex>
//
// This function is intended exclusively for local Wireshark debugging.
// Never set DGPROTO_KEYLOG in production environments.
//
// keylogWrite is safe for concurrent use.
func keylogWrite(sessionID [16]byte, sendKey, recvKey [KeySize]byte) {
	keylogOnce.Do(keylogOpen)
	if !keylogActive {
		return
	}
	sid := hex.EncodeToString(sessionID[:])
	sk := hex.EncodeToString(sendKey[:])
	rk := hex.EncodeToString(recvKey[:])
	fmt.Fprintf(keylogWriter,
		"DGPROTO_SESSION_KEY %s send 1 %s\nDGPROTO_SESSION_KEY %s recv 1 %s\n",
		sid, sk, sid, rk,
	)
}
