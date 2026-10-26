package dgproto

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

// temporaryAcceptError is a synthetic transient accept error used in tests.
// It does not implement the deprecated Temporary() method; isTemporaryAcceptError
// must recognise it via the net.Error interface (Timeout) or the sentinel flag.
type temporaryAcceptError struct{}

func (temporaryAcceptError) Error() string     { return "temporary accept error" }
func (temporaryAcceptError) Timeout() bool     { return false }
func (temporaryAcceptError) IsTemporary() bool { return true }

// TestIsTemporaryAcceptError verifies the classification function used by Serve.
func TestIsTemporaryAcceptError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"temporaryAcceptError", temporaryAcceptError{}, true},
		{"net timeout", &net.OpError{Op: "accept", Err: &timeoutError{}}, true},
		{"net non-timeout", &net.OpError{Op: "accept", Err: errors.New("permanent")}, false},
		{"wrapped timeout", fmt.Errorf("accept: %w", timeoutError{}), true},
		{"wrapped non-timeout", fmt.Errorf("accept: %w", &net.OpError{Op: "accept", Err: errors.New("permanent")}), false},
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"wrapped ECONNABORTED", fmt.Errorf("accept: %w", syscall.ECONNABORTED), true},
		{"EMFILE", syscall.EMFILE, true},
		{"wrapped EMFILE", fmt.Errorf("accept: %w", syscall.EMFILE), true},
		{"ENFILE", syscall.ENFILE, true},
		{"wrapped ENFILE", fmt.Errorf("accept: %w", syscall.ENFILE), true},
		{"other errno", syscall.EINVAL, false},
		{"wrapped other errno", fmt.Errorf("accept: %w", syscall.EINVAL), false},
		{"closed", net.ErrClosed, false},
		{"wrapped closed", fmt.Errorf("accept: %w", net.ErrClosed), false},
		{"marker false", acceptMarkerError(false), false},
		{"wrapped marker alone", fmt.Errorf("accept: %w", temporaryAcceptError{}), false},
		{"deprecated Temporary only", deprecatedTemporaryAcceptError{}, false},
		{"wrapped deprecated Temporary only", fmt.Errorf("accept: %w", deprecatedTemporaryAcceptError{}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTemporaryAcceptError(tc.err); got != tc.want {
				t.Fatalf("isTemporaryAcceptError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// acceptMarkerError deliberately implements only the direct synthetic marker.
type acceptMarkerError bool

func (acceptMarkerError) Error() string       { return "accept marker" }
func (e acceptMarkerError) IsTemporary() bool { return bool(e) }

// Temporary alone must not make an accept error retryable.
type deprecatedTemporaryAcceptError struct{}

func (deprecatedTemporaryAcceptError) Error() string   { return "deprecated temporary accept error" }
func (deprecatedTemporaryAcceptError) Timeout() bool   { return false }
func (deprecatedTemporaryAcceptError) Temporary() bool { return true }

// timeoutError is a minimal net.Error that reports Timeout() == true.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

type scriptedListener struct {
	errors []error
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	err := l.errors[0]
	l.errors = l.errors[1:]
	return nil, err
}

func (*scriptedListener) Close() error   { return nil }
func (*scriptedListener) Addr() net.Addr { return nil }

func TestServerServeRetriesTemporaryAcceptError(t *testing.T) {
	server, err := NewServer(validServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	permanent := errors.New("permanent accept error")
	listener := &scriptedListener{errors: []error{temporaryAcceptError{}, permanent}}

	if err := server.Serve(listener); !errors.Is(err, permanent) {
		t.Fatalf("Serve error = %v, want %v after retry", err, permanent)
	}
}

// blockingListener blocks Accept until its release channel is closed, then
// returns a temporary error. Close is a no-op.
type blockingListener struct {
	release chan struct{}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.release
	return nil, temporaryAcceptError{}
}

func (*blockingListener) Close() error   { return nil }
func (*blockingListener) Addr() net.Addr { return nil }

func TestServerServeCloseDuringTemporaryBackoff(t *testing.T) {
	server, err := NewServer(validServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}

	// The listener blocks Accept until we release it, then returns a temporary
	// error so Serve enters the backoff sleep. We then call Close() and verify
	// that Serve returns ErrServerClosed promptly rather than waiting out the
	// full backoff duration.
	release := make(chan struct{})
	listener := &blockingListener{release: release}

	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()

	// Unblock Accept so Serve receives the temporary error and enters backoff.
	close(release)

	// Close the server while Serve is sleeping in the backoff select.
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()

	select {
	case err := <-serveDone:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Serve error = %v, want %v", err, ErrServerClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not return after Close() during temporary-error backoff")
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close error = %v", err)
	}
}
