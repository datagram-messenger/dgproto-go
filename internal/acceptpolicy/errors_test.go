package acceptpolicy_test

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/datagram-messenger/dgproto-go/internal/acceptpolicy"
)

type timeoutError bool

func (timeoutError) Error() string   { return "listener error" }
func (e timeoutError) Timeout() bool { return bool(e) }
func (timeoutError) Temporary() bool { return false }

type markerError bool

func (markerError) Error() string       { return "synthetic marker" }
func (e markerError) IsTemporary() bool { return bool(e) }

type temporaryOnlyError struct{}

func (temporaryOnlyError) Error() string   { return "deprecated temporary" }
func (temporaryOnlyError) Timeout() bool   { return false }
func (temporaryOnlyError) Temporary() bool { return true }

func TestRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"permanent", errors.New("permanent"), false},
		{"timeout", timeoutError(true), true},
		{"non-timeout", timeoutError(false), false},
		{"net.OpError timeout", &net.OpError{Op: "accept", Err: timeoutError(true)}, true},
		{"net.OpError permanent", &net.OpError{Op: "accept", Err: errors.New("permanent")}, false},
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"EMFILE", syscall.EMFILE, true},
		{"ENFILE", syscall.ENFILE, true},
		{"other errno", syscall.EINVAL, false},
		{"closed", net.ErrClosed, false},
		{"deprecated Temporary only", temporaryOnlyError{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptpolicy.Retryable(tc.err); got != tc.want {
				t.Fatalf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if tc.err != nil {
				wrapped := fmt.Errorf("accept: %w", tc.err)
				if got := acceptpolicy.Retryable(wrapped); got != tc.want {
					t.Fatalf("Retryable(wrapped %v) = %v, want %v", tc.err, got, tc.want)
				}
			}
		})
	}
}

func TestRetryableDirectMarker(t *testing.T) {
	for _, value := range []bool{false, true} {
		err := markerError(value)
		if got := acceptpolicy.Retryable(err); got != value {
			t.Fatalf("Retryable(marker %v) = %v", value, got)
		}
		// The marker is a direct structural assertion, not errors.As traversal.
		if acceptpolicy.Retryable(fmt.Errorf("accept: %w", err)) {
			t.Fatalf("wrapped marker %v alone must not be retryable", value)
		}
	}
}
