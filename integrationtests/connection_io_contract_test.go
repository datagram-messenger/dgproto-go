package integrationtests

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Returns a positive prefix together with an error, not a successful short I/O.
// The gate lets tests populate accepted and capacity-blocked waiters first.
type terminalIOConn struct {
	net.Conn
	readFailure bool
	failure     error
	release     chan struct{}
	entered     chan struct{}
	once        sync.Once
	reads       atomic.Int32
	writes      atomic.Int32
}

func (c *terminalIOConn) Read(p []byte) (int, error) {
	c.reads.Add(1)
	if c.readFailure {
		<-c.release
		copy(p, []byte{1, 2, 3})
		return 3, c.failure
	}
	return c.Conn.Read(p)
}

// net.Pipe deadlines/Close release the nonfailing read. No extra goroutine is
// needed for the scripted read; tests always release it before runtime cleanup.
func (c *terminalIOConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	c.once.Do(func() { close(c.entered) })
	if !c.readFailure {
		<-c.release
		return 3, c.failure
	}
	return c.Conn.Write(p)
}

func TestRuntimePositivePartialIOTerminalCauseAndWaiters(t *testing.T) {
	for _, read := range []bool{false, true} {
		name := "write"
		if read {
			name = "read"
		}
		t.Run(name, func(t *testing.T) {
			sentinel := errors.New("scripted positive partial " + name)
			a, b := net.Pipe()
			t.Cleanup(func() { _ = b.Close() })
			local, _ := runtimeSessions(t)
			socket := &terminalIOConn{Conn: a, readFailure: read, failure: sentinel, release: make(chan struct{}), entered: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(socket.release) }) }
			c := runtimeConnection(t, socket, local, dgproto.ConnectionConfig{OutboundQueue: 1})
			t.Cleanup(release)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Start(parent)
			active := make(chan error, 1)
			go func() { active <- c.SendAndWait(context.Background(), dgproto.Ack{Sequences: []uint64{1}}) }()
			receiveBounded(t, socket.entered)
			queuedCtx := &completionContext{Context: context.Background(), accepted: make(chan struct{})}
			queued := make(chan error, 1)
			go func() { queued <- c.SendAndWait(queuedCtx, dgproto.Ack{Sequences: []uint64{2}}) }()
			receiveBounded(t, queuedCtx.accepted)
			blockedCtx := &observingContext{Context: context.Background(), observed: make(chan struct{})}
			blocked := make(chan error, 1)
			go func() { blocked <- c.SendAndWait(blockedCtx, dgproto.Ack{Sequences: []uint64{3}}) }()
			receiveBounded(t, blockedCtx.observed)
			release()
			receiveBounded(t, c.Done())
			if !errors.Is(c.Err(), sentinel) {
				t.Fatalf("first cause = %v", c.Err())
			}
			if err := receiveBounded(t, active); err == nil || (!read && !errors.Is(err, sentinel)) {
				t.Fatalf("active write after terminal %s = %v", name, err)
			}
			// Accepted queued work observes the terminal cause; capacity waiters were
			// never accepted and instead observe ErrConnectionClosed.
			if err := receiveBounded(t, queued); !errors.Is(err, sentinel) {
				t.Fatalf("accepted waiter = %v", err)
			}
			if err := receiveBounded(t, blocked); !errors.Is(err, dgproto.ErrConnectionClosed) {
				t.Fatalf("nonaccepted waiter = %v", err)
			}
			if !local.Closed() {
				t.Fatal("terminal I/O left session reusable")
			}
			if socket.writes.Load() != 1 {
				t.Fatalf("write retries = %d", socket.writes.Load())
			}
			if read && socket.reads.Load() != 1 {
				t.Fatalf("read retries = %d", socket.reads.Load())
			}
			// Introduce later cancellation, peer EOF and local Close only AFTER the
			// first cause has been observed, never asserting a race winner.
			first := c.Err()
			cancel()
			_ = b.Close()
			_ = c.Close()
			if c.Err() != first {
				t.Fatalf("first cause replaced: %v -> %v", first, c.Err())
			}
			if err := c.SendAndWait(context.Background(), dgproto.Ack{Sequences: []uint64{4}}); !errors.Is(err, dgproto.ErrConnectionClosed) {
				t.Fatalf("terminal send = %v", err)
			}
			if socket.writes.Load() != 1 {
				t.Fatal("terminal send touched stream")
			}
			buf := make([]byte, 1)
			if _, err := a.Read(buf); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("underlying stream not closed: %v", err)
			}
		})
	}
}
