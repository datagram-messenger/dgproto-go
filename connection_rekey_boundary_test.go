package dgproto

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// These tests need private trigger/commit state, but exercise sends through the
// public runtime. Synthetic sessions and pipe gates avoid waiting for a real
// frame-count/time trigger or guessing when the single writer is scheduled.
func TestConnectionCallerCancellationDuringRekeyDoesNotRetract(t *testing.T) {
	for _, pad := range []uint8{0, 255} {
		t.Run(stringNamePadding(pad), func(t *testing.T) {
			local, remote := net.Pipe()
			sender, receiver := testSessions(t)
			sender.sentInEpoch = sender.rekeyFrameLimit
			entered := make(chan struct{})
			c := NewConnection(NewTCPTransport(&gatedIOConn{Conn: local, entered: entered, write: true}), sender, ConnectionConfig{})
			peer := NewTCPTransport(remote)
			defer func() { _ = peer.Close(); _ = c.Close(); waitConnection(t, c) }()
			c.Start(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- c.SendPaddedAndWait(ctx, Ack{Sequences: []uint64{17}}, pad) }()
			receiveBounded(t, entered)
			sender.sendMu.Lock()
			pending, epoch := sender.pendingRekey != nil, sender.sendEpoch
			sender.sendMu.Unlock()
			if !pending || epoch != 1 {
				t.Fatal("rekey committed before its local write completed")
			}
			cancel()
			if err := receiveBounded(t, result); !errors.Is(err, context.Canceled) {
				t.Fatalf("caller cancellation = %v", err)
			}
			if c.Err() != nil {
				t.Fatalf("caller cancellation terminated runtime: %v", c.Err())
			}
			readCtx, stop := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer stop()
			for i, want := range []MessageType{MessageTypeRekeyInit, MessageTypeAck} {
				frame, err := peer.ReadFrame(readCtx)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Header.MessageType != want || frame.Header.Sequence != 1 || frame.Header.PadLength != pad {
					t.Fatalf("frame %d: unexpected header %+v", i, frame.Header)
				}
				message, err := receiver.Receive(frame)
				if err != nil {
					t.Fatal(err)
				}
				if ack, ok := message.(*Ack); ok && (len(ack.Sequences) != 1 || ack.Sequences[0] != 17) {
					t.Fatalf("original message changed: %#v", ack)
				}
			}
			next := make(chan error, 1)
			go func() { next <- c.SendAndWait(readCtx, Ack{Sequences: []uint64{18}}) }()
			frame, err := peer.ReadFrame(readCtx)
			if err != nil {
				t.Fatal(err)
			}
			message, err := receiver.Receive(frame)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Header.Sequence != 2 || message.(*Ack).Sequences[0] != 18 {
				t.Fatal("original message was lost, retried or reordered across rekey")
			}
			if err := receiveBounded(t, next); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConnectionRuntimeCancellationDuringRekeyFailsClosed(t *testing.T) {
	local, remote := net.Pipe()
	sender, _ := testSessions(t)
	sender.sentInEpoch = sender.rekeyFrameLimit
	oldKey := sender.sendKey
	entered := make(chan struct{})
	c := NewConnection(NewTCPTransport(&gatedIOConn{Conn: local, entered: entered, write: true}), sender, ConnectionConfig{})
	defer func() { _ = remote.Close(); _ = c.Close(); waitConnection(t, c) }()
	runtimeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(runtimeCtx)
	result := make(chan error, 1)
	go func() { result <- c.SendAndWait(context.Background(), Ack{Sequences: []uint64{20}}) }()
	receiveBounded(t, entered)
	cancel()
	if err := receiveBounded(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime-canceled completion = %v", err)
	}
	waitConnection(t, c)
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatalf("runtime cancellation lost first cause: %v", c.Err())
	}
	sender.sendMu.Lock()
	epoch, key, pending := sender.sendEpoch, sender.sendKey, sender.pendingRekey != nil
	sender.sendMu.Unlock()
	if epoch != 1 || key != oldKey || !pending {
		t.Fatal("runtime cancellation committed an unwritten rekey")
	}
	if _, err := sender.Send(Ack{Sequences: []uint64{21}}, 0); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("failed stream's session remained usable: %v", err)
	}
}

func stringNamePadding(pad uint8) string {
	if pad == 0 {
		return "unpadded"
	}
	return "maximum padding"
}

type rekeyFailConn struct {
	net.Conn
	writes int // Only TCPTransport's serialized write lane accesses this field.
	failAt int
	prefix int
	cause  error
}

func (c *rekeyFailConn) Write(p []byte) (int, error) {
	c.writes++
	if c.writes != c.failAt {
		return c.Conn.Write(p)
	}
	if c.prefix == 0 {
		return 0, c.cause
	}
	n, err := c.Conn.Write(p[:c.prefix])
	if err != nil {
		return n, err
	}
	return n, c.cause
}

func TestConnectionRekeyWriteFailureCommitBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		prefix int
	}{
		{"rekey before bytes", 1, 0},
		{"partial rekey header", 1, HeaderSize - 1},
		{"original after committed rekey", 2, 0},
		{"partial original header", 2, HeaderSize - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local, remote := net.Pipe()
			sender, receiver := testSessions(t)
			sender.sentInEpoch = sender.rekeyFrameLimit
			oldKey := sender.sendKey
			failure := errors.New("synthetic local write failure")
			c := NewConnection(NewTCPTransport(&rekeyFailConn{Conn: local, failAt: tc.failAt, prefix: tc.prefix, cause: failure}), sender, ConnectionConfig{})
			peer := NewTCPTransport(remote)
			defer func() { _ = peer.Close(); _ = c.Close(); waitConnection(t, c) }()
			c.Start(context.Background())
			result := make(chan error, 1)
			go func() { result <- c.SendAndWait(context.Background(), Ack{Sequences: []uint64{19}}) }()
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			if tc.failAt == 2 {
				frame, err := peer.ReadFrame(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Header.MessageType != MessageTypeRekeyInit {
					t.Fatal("application frame overtook rekey")
				}
				if _, err := receiver.Receive(frame); err != nil {
					t.Fatal(err)
				}
			}
			if tc.prefix != 0 {
				if err := remote.SetReadDeadline(time.Now().Add(connectionTestTimeout)); err != nil {
					t.Fatal(err)
				}
				if _, err := io.ReadFull(remote, make([]byte, tc.prefix)); err != nil {
					t.Fatal(err)
				}
			}
			if err := receiveBounded(t, result); !errors.Is(err, failure) {
				t.Fatalf("write completion = %v, want injected failure", err)
			}
			waitConnection(t, c)
			if !errors.Is(c.Err(), failure) {
				t.Fatalf("terminal cause = %v", c.Err())
			}
			sender.sendMu.Lock()
			epoch, key, pending, sequence := sender.sendEpoch, sender.sendKey, sender.pendingRekey != nil, sender.nextSequence
			sender.sendMu.Unlock()
			if tc.failAt == 1 {
				if epoch != 1 || key != oldKey || !pending || sequence != 2 {
					t.Fatal("failed rekey write committed a new key or rolled back its nonce")
				}
			} else if epoch != 2 || key == oldKey || pending || sequence != 2 {
				t.Fatal("successful rekey was rolled back or failed original nonce was reused")
			}
			if err := c.Send(Ack{}); !errors.Is(err, ErrConnectionClosed) {
				t.Fatalf("terminal runtime accepted retry: %v", err)
			}
		})
	}
}
