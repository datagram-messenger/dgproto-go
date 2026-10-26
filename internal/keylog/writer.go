//go:build dgproto_keylog

// Package keylog owns the optional process-wide traffic-secret log sink.
// It is for local debugging only, never production use. Callers decide when
// session material is committed; this package only serializes supplied records.
package keylog

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"
)

// once guards one-time initialization. mu is a leaf lock: nothing else is
// acquired while it is held. The file stays open for the process lifetime.
var (
	once   sync.Once
	mu     sync.Mutex
	writer *os.File
	active bool
)

func open() {
	path := os.Getenv("DGPROTO_KEYLOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	writer = f
	active = true
}

// Write appends both initial epoch-1 keys, without interleaving their lines.
// The environment is consulted once, even when absent or opening fails.
// Write is safe for concurrent use. Keys are borrowed only for this call.
func Write(sessionID [16]byte, sendKey, recvKey [32]byte) {
	once.Do(open)
	if !active {
		return
	}
	sid := hex.EncodeToString(sessionID[:])
	mu.Lock()
	fmt.Fprintf(writer, "DGPROTO_SESSION_KEY %s send 1 %s\n", sid, hex.EncodeToString(sendKey[:]))
	fmt.Fprintf(writer, "DGPROTO_SESSION_KEY %s recv 1 %s\n", sid, hex.EncodeToString(recvKey[:]))
	mu.Unlock()
}

// WriteEpoch appends one committed directional epoch using the same sink.
// It preserves the supplied direction and epoch; session policy belongs to callers.
func WriteEpoch(sessionID [16]byte, direction string, epoch uint32, key [32]byte) {
	once.Do(open)
	if !active {
		return
	}
	mu.Lock()
	fmt.Fprintf(writer, "DGPROTO_SESSION_KEY %s %s %d %s\n",
		hex.EncodeToString(sessionID[:]), direction, epoch, hex.EncodeToString(key[:]))
	mu.Unlock()
}
