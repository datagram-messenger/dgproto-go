package integrationtests

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Accept is scripted, but handshakes, encrypted traffic and Server ownership are
// real. synctest.Wait establishes callback completion and capacity release;
// peer EOF alone would not establish that boundary.
type lifecycleListener struct {
	sockets chan net.Conn
	done    chan struct{}
	once    sync.Once
}

func (l *lifecycleListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.sockets:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *lifecycleListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *lifecycleListener) Addr() net.Addr { return &net.TCPAddr{} }

type admissionEvent struct {
	conn *dgproto.Connection
	info dgproto.AdmissionInfo
}
type disconnectEvent struct {
	admissionEvent
	cause      error
	background bool
}

func serverTestKey(t *testing.T, value byte) dgproto.StaticKey {
	t.Helper()
	key, err := dgproto.LoadStaticKey(bytes.Repeat([]byte{value}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func connectScriptedServer(t *testing.T, l *lifecycleListener, serverKey, clientKey dgproto.StaticKey) (*dgproto.TCPTransport, *dgproto.Session) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	select {
	case l.sockets <- b:
	case <-l.done:
		t.Fatal("listener closed")
	case <-time.After(connectionTestTimeout):
		t.Fatal("server did not accept")
	}
	tr := dgproto.NewTCPTransport(a)
	hs, err := dgproto.NewInitiatorHandshake(clientKey, serverKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
	defer cancel()
	for flight := 0; flight < 2; flight++ {
		payload, err := hs.WriteFlight()
		if err != nil {
			t.Fatal(err)
		}
		kind := dgproto.MessageTypeHandshakeInit
		if flight == 1 {
			kind = dgproto.MessageTypeHandshakeResponse
		}
		frame, err := dgproto.NewFrame(kind, [16]byte{}, 0, payload, make([]byte, dgproto.AEADTagSize), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.WriteFrame(ctx, frame); err != nil {
			t.Fatal(err)
		}
		if flight == 0 {
			response, err := tr.ReadFrame(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := hs.ReadFlight(response.Payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	result, err := hs.Result()
	if err != nil {
		t.Fatal(err)
	}
	session, err := dgproto.NewSessionFromHandshake(dgproto.CipherChaCha20Poly1305, result)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return tr, session
}

func TestServerAdmissionDisconnectContainmentAndCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		serverKey, clientKey := serverTestKey(t, 4), serverTestKey(t, 5)
		listener := &lifecycleListener{sockets: make(chan net.Conn), done: make(chan struct{})}
		admitted := make(chan admissionEvent, 8)
		disconnected := make(chan disconnectEvent, 8)
		dispatched := make(chan *dgproto.Connection, 8)
		gate := make(chan struct{})
		var gateOnce sync.Once
		releaseGate := func() { gateOnce.Do(func() { close(gate) }) }
		rejection := errors.New("admission denied")
		handlerFailure := errors.New("handler failed")
		var admissions atomic.Int32
		var dispatches atomic.Int32
		var server *dgproto.Server
		server, err := dgproto.NewServer(dgproto.ServerConfig{
			StaticKey: serverKey, MaxConcurrentHandshakes: 1, MaxActiveConnections: 1,
			Admission: func(ctx context.Context, c *dgproto.Connection, info dgproto.AdmissionInfo) error {
				_ = server.Stats()
				admitted <- admissionEvent{c, info}
				switch admissions.Add(1) {
				case 1:
					return rejection
				case 2:
					panic("admission panic")
				case 3:
					select {
					case <-gate:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				default:
					return nil
				}
			},
			Handler: func(ctx context.Context, c *dgproto.Connection, message any) error {
				dispatched <- c
				if dispatches.Add(1) == 1 {
					return handlerFailure
				}
				return c.SendAndWait(ctx, message)
			},
			OnDisconnect: func(ctx context.Context, c *dgproto.Connection, info dgproto.AdmissionInfo, cause error) {
				_ = server.Stats()
				_, deadline := ctx.Deadline()
				disconnected <- disconnectEvent{admissionEvent{c, info}, cause, ctx.Err() == nil && ctx.Done() == nil && !deadline}
				panic("hook panic must not terminate Serve")
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		t.Cleanup(func() {
			releaseGate()
			_ = server.Abort()
			closed := make(chan error, 1)
			go func() { closed <- server.Close() }()
			receiveBounded(t, closed)
			if err := receiveBounded(t, served); !errors.Is(err, dgproto.ErrServerClosed) {
				t.Errorf("Serve = %v", err)
			}
		})
		for range 2 {
			rejectedTransport, _ := connectScriptedServer(t, listener, serverKey, clientKey)
			event := receiveBounded(t, admitted)
			synctest.Wait()
			receiveBounded(t, event.conn.Done())
			if event.conn.Err() == nil {
				t.Fatal("rejected connection not terminated")
			}
			ctx, cancel := context.WithTimeout(context.Background(), connectionTestTimeout)
			_, err := rejectedTransport.ReadFrame(ctx)
			cancel()
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("rejected socket not closed: %v", err)
			}
			if dispatches.Load() != 0 || len(disconnected) != 0 {
				t.Fatal("rejected connection dispatched or invoked OnDisconnect")
			}
			want := dgproto.ServerStats{HandshakeSlotsCapacity: 1, ConnectionSlotsCapacity: 1}
			if got := server.Stats(); got != want {
				t.Fatalf("rejection/panic leaked resources: %+v", got)
			}
		}
		tr, session := connectScriptedServer(t, listener, serverKey, clientKey)
		event := receiveBounded(t, admitted)
		if event.info.SessionID != session.SessionID() || !bytes.Equal(event.info.PeerStatic[:], clientKey.Public()) || event.info.RemoteAddr == nil {
			t.Fatal("admission identity mismatch")
		}
		frame, err := session.Send(dgproto.Ack{Sequences: []uint64{7}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		writing := make(chan error, 1)
		go func() { writing <- tr.WriteFrame(context.Background(), frame) }()
		// All bubble goroutines are durably blocked. Encrypted application traffic
		// cannot be dispatched while the Admission callback remains gated.
		synctest.Wait()
		if len(dispatched) != 0 || event.conn.Err() != nil {
			t.Fatal("dispatch or termination before admission succeeded")
		}
		releaseGate()
		if err := receiveBounded(t, writing); err != nil {
			t.Fatal(err)
		}
		if receiveBounded(t, dispatched) != event.conn {
			t.Fatal("handler connection mismatch")
		}
		hook := receiveBounded(t, disconnected)
		synctest.Wait()
		if hook.conn != event.conn || hook.info != event.info || !errors.Is(hook.cause, handlerFailure) || hook.cause != event.conn.Err() || !hook.background {
			t.Fatalf("disconnect mismatch: %+v", hook)
		}
		// A fresh full handshake with both limits still at one proves admission and
		// hook panics did not strand either capacity pool or the accept loop.
		nextTr, nextSession := connectScriptedServer(t, listener, serverKey, clientKey)
		nextEvent := receiveBounded(t, admitted)
		nextFrame, err := nextSession.Send(dgproto.Ack{Sequences: []uint64{8}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := nextTr.WriteFrame(context.Background(), nextFrame); err != nil {
			t.Fatal(err)
		}
		readRuntimeAck(t, nextTr, nextSession, 8)
		if receiveBounded(t, dispatched) != nextEvent.conn {
			t.Fatal("next handler identity mismatch")
		}
		_ = nextTr.Close()
		nextHook := receiveBounded(t, disconnected)
		synctest.Wait()
		if nextHook.conn != nextEvent.conn || nextHook.info != nextEvent.info || !nextHook.background || nextHook.cause != nextEvent.conn.Err() {
			t.Fatal("next disconnect mismatch")
		}
		synctest.Wait()
		if len(disconnected) != 0 || admissions.Load() != 4 || dispatches.Load() != 2 {
			t.Fatal("duplicate disconnect or unexpected dispatch")
		}
		want := dgproto.ServerStats{HandshakeSlotsCapacity: 1, ConnectionSlotsCapacity: 1}
		if got := server.Stats(); got != want {
			t.Fatalf("disconnect panic leaked resources: %+v", got)
		}
	})
}
