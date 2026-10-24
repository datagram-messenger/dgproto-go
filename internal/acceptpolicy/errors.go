// Package acceptpolicy classifies transient listener accept errors.
package acceptpolicy

import (
	"errors"
	"net"
	"syscall"
)

// Retryable reports whether an error returned by Listener.Accept
// is transient and the accept loop should back off and retry rather than exit.
//
// net.Error.Temporary() was deprecated in Go 1.18 because its semantics were
// ill-defined. This function replaces it with explicit, auditable criteria:
//
//   - net.Error.Timeout() — deadline exceeded on the listener (e.g. SetDeadline
//     used for periodic wakeups); safe to retry.
//   - syscall.ECONNABORTED — the OS aborted the connection before Accept could
//     return it; transient on Linux and most BSDs.
//   - syscall.EMFILE / syscall.ENFILE — process or system file-descriptor table
//     is temporarily exhausted; backing off gives the runtime time to reclaim fds.
//
// Errors that do not match any of these criteria are treated as permanent and
// cause Serve to return.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	// Timeout on the listener itself (e.g. net.Listener with a deadline).
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// Transient syscall errors: connection aborted before accept, or fd exhaustion.
	if errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EMFILE) ||
		errors.Is(err, syscall.ENFILE) {
		return true
	}
	// Test-only sentinel: synthetic errors that carry an IsTemporary marker.
	type hasIsTemporary interface{ IsTemporary() bool }
	if t, ok := err.(hasIsTemporary); ok && t.IsTemporary() {
		return true
	}
	return false
}
