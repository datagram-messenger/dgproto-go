package dgproto

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

// A locally written reply followed by SessionClose need not reach a queued
// application callback. Hold the preceding callback to make this boundary
// deterministic without sleeps or changes to the shutdown contract.
func TestConnectionReplyThenCloseDropsQueuedCallback(t *testing.T) {
	entered := make(chan struct{})
	var replies atomic.Int32
	c, peerSession, peer, cleanup := testConnectionPair(t, ConnectionConfig{
		Handler: func(ctx context.Context, _ *Connection, message any) error {
			data, ok := message.(*EncryptedData)
			if !ok {
				return nil
			}
			if data.AppMessageType == 1 {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}
			replies.Add(1)
			return nil
		},
	})
	defer cleanup()
	c.Start(context.Background())
	writeConnectionMessage(t, peer, peerSession, EncryptedData{StreamID: 1, AppMessageType: 1})
	receiveBounded(t, entered)
	writeConnectionMessage(t, peer, peerSession, EncryptedData{StreamID: 1, AppMessageType: 2})
	writeConnectionMessage(t, peer, peerSession, SessionClose{})
	waitConnection(t, c)
	if replies.Load() != 0 || len(c.handlerQueue) != 0 {
		t.Fatal("queued reply ran or remained after shutdown")
	}
	if !errors.Is(c.Err(), ErrConnectionClosed) {
		t.Fatalf("terminal cause: %v", c.Err())
	}
}
