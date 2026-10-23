package dgproto

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDialContextPinnedStoppedLifetimeExchange(t *testing.T) {
	config, key := clientTestConfig(t)
	admitted := make(chan AdmissionInfo, 1)
	received := make(chan *Ack, 1)
	config.Handler = func(ctx context.Context, c *Connection, message any) error {
		if ack, ok := message.(*Ack); ok {
			select {
			case received <- ack:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	server, err := NewServer(ServerConfig{StaticKey: key, AllowedClients: [][]byte{config.StaticKey.Public()}, Admission: func(ctx context.Context, c *Connection, info AdmissionInfo) error { admitted <- info; return nil }, Handler: func(ctx context.Context, c *Connection, message any) error {
		if ack, ok := message.(*Ack); ok {
			return c.SendPaddedAndWait(ctx, *ack, 1)
		}
		return nil
	}, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		server.Abort()
		_ = server.Close()
		select {
		case <-serveDone:
		case <-time.After(connectionTestTimeout):
			t.Error("Serve stuck")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	c, info, err := DialContext(ctx, listener.Addr().String(), config)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.started.Load() {
		t.Fatal("dial started runtime")
	}
	select {
	case <-c.Done():
		t.Fatal("establishment cancellation killed lifetime")
	default:
	}
	select {
	case serverInfo := <-admitted:
		if serverInfo.SessionID != info.SessionID || serverInfo.PeerStatic != config.StaticKey.public || info.PeerStatic != key.public {
			t.Fatal("metadata mismatch")
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("admission stuck")
	}
	if err = c.SendPadded(Ack{Sequences: []uint64{42}}, 255); err != nil {
		t.Fatal(err)
	}
	runtime, cancelRuntime := context.WithCancel(context.Background())
	defer cancelRuntime()
	c.Start(runtime)
	select {
	case ack := <-received:
		if len(ack.Sequences) != 1 || ack.Sequences[0] != 42 {
			t.Fatal("wrong echo")
		}
	case <-time.After(connectionTestTimeout):
		t.Fatal("exchange stuck")
	}
	cancelRuntime()
	waitConnection(t, c)
	if !errors.Is(c.Err(), context.Canceled) {
		t.Fatalf("terminal=%v", c.Err())
	}
}

func TestDialContextWrongPinWithholdsFinalFlight(t *testing.T) {
	config, key := clientTestConfig(t)
	wrong, err := GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	config.ServerStatic = wrong.Public()
	local, peer := net.Pipe()
	defer peer.Close()
	counted := &clientCountConn{Conn: local}
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	peerDone := make(chan error, 1)
	go func() {
		transport := NewTCPTransport(peer)
		h, e := NewResponderHandshake(key, nil)
		if e != nil {
			peerDone <- e
			return
		}
		first, e := transport.ReadFrame(ctx)
		if e != nil {
			peerDone <- e
			return
		}
		if e = h.ReadFlight(first.Payload); e != nil {
			peerDone <- e
			return
		}
		flight, e := h.WriteFlight()
		if e != nil {
			peerDone <- e
			return
		}
		frame, e := NewFrame(MessageTypeHandshakeResponse, [16]byte{}, 0, flight, make([]byte, AEADTagSize), nil)
		if e != nil {
			peerDone <- e
			return
		}
		if e = transport.WriteFrame(ctx, frame); e != nil {
			peerDone <- e
			return
		}
		_, e = transport.ReadFrame(ctx)
		peerDone <- e
	}()
	c, info, err := dialContext(ctx, "unused", config, func(context.Context, string, string) (net.Conn, error) { return counted, nil })
	if !errors.Is(err, ErrHandshakeFailed) || c != nil || info != (AdmissionInfo{}) {
		t.Fatalf("result=%v %v %v", c, info, err)
	}
	if counted.closes.Load() != 1 {
		t.Fatal("failure close count")
	}
	select {
	case err := <-peerDone:
		if err == nil {
			t.Fatal("wrong pin sent final flight")
		}
	case <-ctx.Done():
		t.Fatal("peer stuck")
	}
}
