package dgproto

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// clientFinalConn synchronizes on the final-flight write, not elapsed sleep.
type clientFinalConn struct {
	net.Conn
	writes  int
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
}

func (c *clientFinalConn) Write(p []byte) (int, error) {
	c.writes++
	if c.writes == 2 {
		c.once.Do(func() { close(c.entered) })
		if c.fail {
			return 0, io.ErrClosedPipe
		}
		<-c.release
	}
	return c.Conn.Write(p)
}
func (c *clientFinalConn) SetWriteDeadline(d time.Time) error {
	if !d.IsZero() {
		select {
		case <-c.release:
		default:
			close(c.release)
		}
	}
	return c.Conn.SetWriteDeadline(d)
}

func TestDialContextFinalWriteFailureAndCancellation(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure", false: "cancel"}[fail], func(t *testing.T) {
			config, key := clientTestConfig(t)
			local, peer := net.Pipe()
			defer peer.Close()
			seam := &clientFinalConn{Conn: local, entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
			counted := &clientCountConn{Conn: seam}
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			peerDone := make(chan error, 1)
			go func() {
				transport := NewTCPTransport(peer)
				h, err := NewResponderHandshake(key, nil)
				if err != nil {
					peerDone <- err
					return
				}
				first, err := transport.ReadFrame(ctx)
				if err != nil {
					peerDone <- err
					return
				}
				if err = h.ReadFlight(first.Payload); err != nil {
					peerDone <- err
					return
				}
				flight, err := h.WriteFlight()
				if err != nil {
					peerDone <- err
					return
				}
				frame, err := NewFrame(MessageTypeHandshakeResponse, [16]byte{}, 0, flight, make([]byte, AEADTagSize), nil)
				if err != nil {
					peerDone <- err
					return
				}
				if err = transport.WriteFrame(ctx, frame); err != nil {
					peerDone <- err
					return
				}
				_, err = transport.ReadFrame(ctx)
				peerDone <- err
			}()
			result := make(chan error, 1)
			go func() {
				c, info, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) { return counted, nil })
				if c != nil || info != (AdmissionInfo{}) {
					result <- errors.New("failure leaked result")
					return
				}
				result <- err
			}()
			select {
			case <-seam.entered:
			case <-ctx.Done():
				t.Fatal("final write never entered")
			}
			if !fail {
				cancel()
			}
			select {
			case err := <-result:
				if fail && !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("err=%v", err)
				}
				if !fail && !errors.Is(err, context.Canceled) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(connectionTestTimeout):
				t.Fatal("final write stuck")
			}
			if counted.closes.Load() != 1 {
				t.Fatalf("closes=%d", counted.closes.Load())
			}
			select {
			case err := <-peerDone:
				if err == nil {
					t.Fatal("failure emitted final flight")
				}
			case <-time.After(connectionTestTimeout):
				t.Fatal("peer stuck")
			}
		})
	}
}

func TestDialContextInitialWriteCancellation(t *testing.T) {
	config, _ := clientTestConfig(t)
	local, peer := net.Pipe()
	defer peer.Close()
	counted := &clientCountConn{Conn: local}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, _, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) {
			close(entered)
			return counted, nil
		})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(connectionTestTimeout):
		t.Fatal("dial not entered")
	}
	cancel() // Peer never reads: either pre-write or a blocked first write.
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("first write stuck")
	}
	if counted.closes.Load() != 1 {
		t.Fatal("first-write cleanup count")
	}
}

func TestDialContextMalformedResponses(t *testing.T) {
	for _, kind := range []string{"truncated", "wrong type", "session id", "sequence", "bad noise"} {
		t.Run(kind, func(t *testing.T) {
			config, _ := clientTestConfig(t)
			local, peer := net.Pipe()
			counted := &clientCountConn{Conn: local}
			defer peer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			defer cancel()
			peerDone := make(chan error, 1)
			go func() {
				transport := NewTCPTransport(peer)
				_, err := transport.ReadFrame(ctx)
				if err != nil {
					peerDone <- err
					return
				}
				if kind == "truncated" {
					_, err = peer.Write([]byte("DGP1"))
					_ = peer.Close()
					peerDone <- err
					return
				}
				frame := Frame{Header: NewHeader(MessageTypeHandshakeResponse, [16]byte{}, 0, 96, 0), Payload: make([]byte, 96)}
				switch kind {
				case "wrong type":
					frame.Header.MessageType = MessageTypeHandshakeInit
				case "session id":
					frame.Header.SessionID[0] = 1
				case "sequence":
					frame.Header.Sequence = 1
				}
				err = transport.WriteFrame(ctx, frame)
				peerDone <- err
			}()
			c, info, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) { return counted, nil })
			if !errors.Is(err, ErrHandshakeFailed) || c != nil || info != (AdmissionInfo{}) {
				t.Fatalf("result=%v %v %v", c, info, err)
			}
			if counted.closes.Load() != 1 {
				t.Fatal("failure cleanup count")
			}
			select {
			case <-peerDone:
			case <-ctx.Done():
				t.Fatal("peer stuck")
			}
		})
	}
}

func TestDialContextSuccessfulStoppedCloseOwnsSocket(t *testing.T) {
	config, key := clientTestConfig(t)
	local, peer := net.Pipe()
	defer peer.Close()
	counted := &clientCountConn{Conn: local}
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	peerDone := make(chan error, 1)
	server, _ := NewServer(ServerConfig{StaticKey: key})
	go func() {
		session, _, err := server.performHandshake(ctx, NewTCPTransport(peer))
		if session != nil {
			defer session.Close()
		}
		peerDone <- err
	}()
	c, _, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) { return counted, nil })
	if err != nil {
		t.Fatal(err)
	}
	if counted.closes.Load() != 0 {
		t.Fatal("success closed socket")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	c.Start(context.Background())
	waitConnection(t, c)
	if counted.closes.Load() != 1 {
		t.Fatalf("owner close count=%d", counted.closes.Load())
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("handshake stuck")
	}
}
