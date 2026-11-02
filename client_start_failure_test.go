package dgproto_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestDialAndStartContextValidConfigCancellationAndNetworkError(t *testing.T) {
	address, config := startedClientFixture(t, nil)
	for _, runtimeCanceled := range []bool{false, true} {
		establishment, runtime := context.Background(), context.Background()
		if runtimeCanceled {
			runtime = canceledStartContext()
		} else {
			establishment = canceledStartContext()
		}
		c, info, err := dgproto.DialAndStartContext(establishment, runtime, address, config)
		if c != nil || info != (dgproto.AdmissionInfo{}) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, info, err := dgproto.DialAndStartContext(ctx, ctx, "127.0.0.1:not-a-port", config)
	var network *net.OpError
	if c != nil || info != (dgproto.AdmissionInfo{}) || !errors.As(err, &network) {
		t.Fatalf("network error chain: %v", err)
	}
}

func TestDialAndStartContextCallerCloseJoinsRuntime(t *testing.T) {
	address, config := startedClientFixture(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := dgproto.DialAndStartContext(ctx, ctx, address, config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.Close()
	awaitStartedClient(t, c)
	cause := c.Err()
	if !errors.Is(cause, dgproto.ErrConnectionClosed) {
		t.Fatalf("caller close cause: %v", cause)
	}
	// Subsequent Start/Close cannot reopen the connection or change its cause.
	c.Start(context.Background())
	_ = c.Close()
	if c.Err() != cause {
		t.Fatal("terminal cause changed")
	}
	if err := c.Send(dgproto.Ack{Sequences: []uint64{1}}); !errors.Is(err, dgproto.ErrConnectionClosed) {
		t.Fatalf("send after close: %v", err)
	}
}
