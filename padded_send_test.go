package dgproto

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPaddedContextQueueCancellation(t *testing.T) {
	for _, wait := range []bool{false, true} {
		c, _, _, cleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 1})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		send := func(ctx context.Context) error {
			if wait {
				return c.SendPaddedAndWait(ctx, Ack{Sequences: []uint64{1}}, 255)
			}
			return c.SendPaddedContext(ctx, Ack{Sequences: []uint64{1}}, 255)
		}
		if err := send(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("pre cancel=%v", err)
		}
		if len(c.outbound) != 0 {
			t.Fatal("pre-cancel enqueued")
		}
		if err := c.SendPadded(Ack{Sequences: []uint64{2}}, 1); err != nil {
			t.Fatal(err)
		}
		full, cancelFull := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { result <- send(full) }()
		cancelFull()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("full cancel=%v", err)
			}
		case <-time.After(connectionTestTimeout):
			t.Fatal("send stuck")
		}
		cleanup()
	}
}

func TestPaddedAndWaitRekeyShutdown(t *testing.T) {
	c, session, peer, cleanup := testConnectionPair(t, ConnectionConfig{OutboundQueue: 2})
	defer cleanup()
	c.session.sendMu.Lock()
	c.session.rekeyFrameLimit = 1
	c.session.sendMu.Unlock()
	c.Start(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	for _, nonce := range []uint64{7, 8} {
		result := make(chan error, 1)
		go func() { result <- c.SendPaddedAndWait(ctx, Ack{Sequences: []uint64{nonce}}, 255) }()
		for {
			frame, err := peer.ReadFrame(ctx)
			if err != nil {
				t.Fatal(err)
			}
			message, err := session.Receive(frame)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Header.MessageType == MessageTypeRekeyInit {
				continue
			}
			ack, ok := message.(*Ack)
			if !ok || ack.Sequences[0] != nonce || frame.Header.PadLength != 255 {
				t.Fatalf("unexpected frame/message: %v %T", frame.Header, message)
			}
			break
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("completion stuck")
		}
	}
	_ = peer.Close()
	if err := c.SendPaddedAndWait(ctx, Ack{Sequences: []uint64{9}}, 1); err == nil {
		t.Fatal("shutdown lost error")
	}
	waitConnection(t, c)
}

func TestPaddedAndWaitWritesPadding(t *testing.T) {
	for _, pad := range []uint8{0, 1, 255} {
		c, session, peer, cleanup := testConnectionPair(t, ConnectionConfig{})
		c.Start(context.Background())
		ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
		result := make(chan error, 1)
		go func() { result <- c.SendPaddedAndWait(ctx, Ack{Sequences: []uint64{7}}, pad) }()
		frame, err := peer.ReadFrame(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Header.PadLength != pad {
			t.Fatalf("pad=%d want=%d", frame.Header.PadLength, pad)
		}
		if _, err = session.Receive(frame); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("completion stuck")
		}
		cancel()
		cleanup()
	}
}
