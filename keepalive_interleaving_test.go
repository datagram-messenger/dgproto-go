package dgproto

import (
	"context"
	"errors"
	"math"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// The reader's next header read proves that the previous authenticated frame
// was handled. No production hook or scheduler sleep is needed.
type keepaliveReadBarrier struct {
	net.Conn
	reads     int
	processed chan struct{}
}

func (b *keepaliveReadBarrier) Read(p []byte) (int, error) {
	b.reads++
	if b.reads == 7 {
		close(b.processed)
	} // three frames: header plus body each
	return b.Conn.Read(p)
}

func TestKeepaliveRuntimeEnqueueInterleaving(t *testing.T) {
	local, remote := net.Pipe()
	session, peer := testSessions(t)
	barrier := &keepaliveReadBarrier{Conn: local, processed: make(chan struct{})}
	c := NewConnection(NewTCPTransport(barrier), session, ConnectionConfig{KeepaliveInterval: time.Millisecond, KeepaliveTimeout: 100 * time.Millisecond})
	transport := NewTCPTransport(remote)
	defer transport.Close()
	defer func() { c.abort(ErrConnectionClosed); waitConnection(t, c) }()
	// Maintenance publishes the nonce then waits for the enqueue lease. The
	// reader remains free to authenticate all three Pong responses.
	c.queueMu.Lock()
	locked := true
	defer func() {
		if locked {
			c.queueMu.Unlock()
		}
	}()
	c.Start(context.Background())
	deadline := time.Now().Add(connectionTestTimeout)
	for c.pingNonce.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("maintenance did not attempt Ping")
		}
		runtime.Gosched()
	}
	for _, nonce := range []uint64{99, 1, 99} {
		writeConnectionMessage(t, transport, peer, PingPong{IsResponse: true, Nonce: nonce})
	}
	select {
	case <-barrier.processed:
	case <-time.After(connectionTestTimeout):
		t.Fatal("reader barrier not reached")
	}
	c.queueMu.Unlock()
	locked = false
	ping := readConnectionMessage(t, transport, peer).(*PingPong)
	if ping.Nonce != 1 {
		t.Fatalf("first Ping nonce = %d", ping.Nonce)
	}
	// A second Ping demonstrates that the first was acknowledged, rather than
	// merely checking a snapshot before the timeout could fire.
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	frame, err := transport.ReadFrame(ctx)
	if err != nil {
		t.Fatalf("second Ping read: %v; terminal cause: %v", err, c.Err())
	}
	message, err := peer.Receive(frame)
	if err != nil {
		t.Fatal(err)
	}
	next := message.(*PingPong)
	if next.Nonce != 2 {
		t.Fatalf("next Ping nonce = %d", next.Nonce)
	}
	if c.Err() != nil {
		t.Fatalf("runtime terminated: %v", c.Err())
	}
}

func TestKeepaliveQueuePressureRetry(t *testing.T) {
	c, _, _, cleanup := testConnectionPair(t, ConnectionConfig{KeepaliveInterval: time.Millisecond, KeepaliveTimeout: time.Second})
	defer cleanup()
	for i := 0; i < cap(c.control); i++ {
		c.control <- queuedMessage{message: PingPong{Nonce: 99}}
	}
	joined := make(chan struct{})
	go func() { defer close(joined); c.maintenanceLoop() }()
	defer func() {
		c.abort(ErrConnectionClosed)
		select {
		case <-joined:
		case <-time.After(connectionTestTimeout):
			t.Fatal("maintenance did not join")
		}
	}()
	deadline := time.Now().Add(connectionTestTimeout)
	for c.pingNonce.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("full queue did not retry")
		}
		runtime.Gosched()
	}
	c.abort(ErrConnectionClosed)
	select {
	case <-joined:
	case <-time.After(connectionTestTimeout):
		t.Fatal("maintenance did not join")
	}
	c.pingMu.Lock()
	expected, ack := c.pingExpected, c.pingAcknowledged
	c.pingMu.Unlock()
	if expected != 0 || ack {
		t.Fatal("failed enqueue or shutdown retained state")
	}
}

func TestKeepaliveStateBoundaries(t *testing.T) {
	c, _, _, cleanup := testConnectionPair(t, ConnectionConfig{})
	defer cleanup()
	c.pingNonce.Store(math.MaxUint64)
	if n := c.beginPing(); n != 1 {
		t.Fatalf("rollover nonce %d", n)
	}
	// A filled notification is harmless: state, not the mailbox, is authoritative.
	c.pong <- 99
	c.acknowledgePing(1)
	c.acknowledgePing(99)
	if !c.consumePingAcknowledgement() || c.consumePingAcknowledgement() {
		t.Fatal("acknowledgement was not retained exactly once")
	}
	c.beginPing()
	c.clearPing()
	c.acknowledgePing(2)
	if c.consumePingAcknowledgement() {
		t.Fatal("cleared expectation acknowledged")
	}
	cause := errors.New("synthetic terminal cause")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				c.acknowledgePing(3)
				_ = c.TrySend(EncryptedData{})
				_ = c.Err()
			}
		}()
	}
	close(start)
	c.abort(cause)
	wg.Wait()
	if c.beginPing() != 0 || c.consumePingAcknowledgement() {
		t.Fatal("shutdown retained keepalive state")
	}
	if !errors.Is(c.Err(), cause) {
		t.Fatalf("first cause lost: %v", c.Err())
	}
}
