package dgproto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"
)

func generateTestStaticKey(t *testing.T) StaticKey {
	t.Helper()
	key, err := GenerateStaticKey()
	if err != nil {
		t.Fatalf("failed to generate static key: %v", err)
	}
	return key
}

func validServerConfig(t *testing.T) ServerConfig {
	t.Helper()
	return ServerConfig{StaticKey: generateTestStaticKey(t)}
}

func TestServerHandshakeAndDataTransfer(t *testing.T) {
	serverKey := generateTestStaticKey(t)
	clientKey := generateTestStaticKey(t)

	clientStaticPublic := clientKey.Public()

	receivedData := make(chan []byte, 1)

	handler := MessageHandler(func(ctx context.Context, conn *Connection, msg any) error {
		if data, ok := msg.(*EncryptedData); ok {
			if len(data.Fields) > 0 {
				receivedData <- data.Fields[0].Value
			}
		}
		return nil
	})

	server, err := NewServer(ServerConfig{
		StaticKey:        serverKey,
		AllowedClients:   [][]byte{clientStaticPublic},
		CipherSuite:      CipherChaCha20Poly1305,
		HandshakeTimeout: 2 * time.Second,
		ReadTimeout:      2 * time.Second,
		WriteTimeout:     2 * time.Second,
		Handler:          handler,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial server: %v", err)
	}
	defer clientConn.Close()

	clientTransport := NewTCPTransport(clientConn)

	initiator, err := NewInitiatorHandshake(clientKey, serverKey.Public())
	if err != nil {
		t.Fatalf("failed to create initiator handshake: %v", err)
	}

	ctx := context.Background()

	flight1, err := initiator.WriteFlight()
	if err != nil {
		t.Fatalf("failed to write flight 1: %v", err)
	}

	frame1, err := NewFrame(MessageTypeHandshakeInit, [16]byte{}, 0, flight1, make([]byte, AEADTagSize), nil)
	if err != nil {
		t.Fatalf("failed to create frame 1: %v", err)
	}
	if err := clientTransport.WriteFrame(ctx, frame1); err != nil {
		t.Fatalf("failed to send frame 1: %v", err)
	}

	frame2, err := clientTransport.ReadFrame(ctx)
	if err != nil {
		t.Fatalf("failed to read frame 2: %v", err)
	}
	if frame2.Header.MessageType != MessageTypeHandshakeResponse {
		t.Fatalf("expected message type 0x02, got 0x%02x", frame2.Header.MessageType)
	}
	if err := initiator.ReadFlight(frame2.Payload); err != nil {
		t.Fatalf("failed to read flight 2: %v", err)
	}

	flight3, err := initiator.WriteFlight()
	if err != nil {
		t.Fatalf("failed to write flight 3: %v", err)
	}

	frame3, err := NewFrame(MessageTypeHandshakeResponse, [16]byte{}, 0, flight3, make([]byte, AEADTagSize), nil)
	if err != nil {
		t.Fatalf("failed to create frame 3: %v", err)
	}
	if err := clientTransport.WriteFrame(ctx, frame3); err != nil {
		t.Fatalf("failed to send frame 3: %v", err)
	}

	if !initiator.Complete() {
		t.Fatal("initiator handshake incomplete")
	}

	hsResult, err := initiator.Result()
	if err != nil {
		t.Fatalf("failed to get handshake result: %v", err)
	}

	clientSession, err := NewSessionFromHandshake(CipherChaCha20Poly1305, hsResult)
	if err != nil {
		t.Fatalf("failed to create client session: %v", err)
	}

	testPayload := []byte("hello server")
	tlv, err := NewTLV(1, testPayload)
	if err != nil {
		t.Fatalf("failed to create TLV: %v", err)
	}

	encryptedData := EncryptedData{
		StreamID:       1,
		AppMessageType: 10,
		Fields:         []TLV{tlv},
	}

	dataframe, err := clientSession.Send(encryptedData, 0)
	if err != nil {
		t.Fatalf("failed to encrypt data frame: %v", err)
	}

	if err := clientTransport.WriteFrame(ctx, dataframe); err != nil {
		t.Fatalf("failed to send data frame: %v", err)
	}

	select {
	case payload := <-receivedData:
		if string(payload) != string(testPayload) {
			t.Fatalf("expected %q, got %q", testPayload, payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server to handle encrypted data")
	}

	_ = clientSession.Close()
}

func TestServerHandshakeAdmissionSaturationAndRelease(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxConcurrentHandshakes = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("failed to acquire first handshake slot")
	}
	if server.tryAcquire(server.handshakes) {
		t.Fatal("acquired saturated handshake slot")
	}
	server.release(server.handshakes)
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("handshake slot was not released")
	}
	server.release(server.handshakes)
}

func TestServerConnectionAdmissionSaturationAndRelease(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxActiveConnections = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("failed to acquire first connection slot")
	}
	if server.tryAcquire(server.connSlots) {
		t.Fatal("acquired saturated connection slot")
	}
	server.release(server.connSlots)
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("connection slot was not released")
	}
	server.release(server.connSlots)
}

func TestServerReleasesHandshakeSlotAfterFailure(t *testing.T) {
	server, err := NewServer(func() ServerConfig {
		c := validServerConfig(t)
		c.MaxConcurrentHandshakes = 1
		c.HandshakeTimeout = time.Second
		return c
	}())
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	if !server.tryAcquire(server.handshakes) || !server.registerHandshake(serverConn) {
		t.Fatal("failed to admit handshake")
	}
	done := make(chan struct{})
	go func() {
		server.handleConn(serverConn)
		close(done)
	}()
	_ = clientConn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed handshake did not finish")
	}
	if !server.tryAcquire(server.handshakes) {
		t.Fatal("failed handshake did not release slot")
	}
	server.release(server.handshakes)
}

func TestServerReleasesConnectionSlotAfterClose(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.MaxActiveConnections = 1; return c }())
	if err != nil {
		t.Fatal(err)
	}
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("failed to acquire connection slot")
	}
	server.release(server.connSlots)
	if !server.tryAcquire(server.connSlots) {
		t.Fatal("closed connection did not release slot")
	}
	server.release(server.connSlots)
}

func TestServerCloseInterruptsStalledHandshake(t *testing.T) {
	server, err := NewServer(func() ServerConfig { c := validServerConfig(t); c.HandshakeTimeout = 30 * time.Second; return c }())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		tracked := len(server.handshakeConns)
		server.mu.Unlock()
		if tracked == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handshake was not tracked")
		}
		time.Sleep(time.Millisecond)
	}

	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for HandshakeTimeout")
	}
	select {
	case err := <-serveDone:
		if !errors.Is(err, ErrServerClosed) {
			t.Fatalf("Serve error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
}

func TestServerCloseIsIdempotent(t *testing.T) {
	server, err := NewServer(validServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHandshakeFrame(t *testing.T) {
	valid, err := NewFrame(MessageTypeHandshakeInit, [16]byte{}, 0, make([]byte, HandshakeInitFixedSize), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Frame)
	}{
		{"message type", func(f *Frame) { f.Header.MessageType = MessageTypeHandshakeResponse }},
		{"session ID", func(f *Frame) { f.Header.SessionID[0] = 1 }},
		{"sequence", func(f *Frame) { f.Header.Sequence = 1 }},
		{"padding flag mismatch", func(f *Frame) { f.Header.Flags = FlagPadding }},
		{"payload length mismatch", func(f *Frame) { f.Header.PayloadLength++ }},
	}
	if err := validateHandshakeFrame(valid, MessageTypeHandshakeInit); err != nil {
		t.Fatalf("valid frame rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := valid
			tt.mutate(&frame)
			if err := validateHandshakeFrame(frame, MessageTypeHandshakeInit); !errors.Is(err, ErrHandshakeFailed) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestServerDisallowedClient(t *testing.T) {
	serverKey := generateTestStaticKey(t)
	clientKey := generateTestStaticKey(t)
	allowedKey := generateTestStaticKey(t)

	allowedPublic := allowedKey.Public()

	server, err := NewServer(ServerConfig{
		StaticKey:        serverKey,
		AllowedClients:   [][]byte{allowedPublic},
		CipherSuite:      CipherChaCha20Poly1305,
		HandshakeTimeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = server.Serve(listener)
	})
	defer func() {
		_ = server.Close()
		wg.Wait()
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer clientConn.Close()

	clientTransport := NewTCPTransport(clientConn)

	initiator, err := NewInitiatorHandshake(clientKey, serverKey.Public())
	if err != nil {
		t.Fatalf("failed to create initiator handshake: %v", err)
	}

	ctx := context.Background()

	flight1, _ := initiator.WriteFlight()
	frame1, _ := NewFrame(MessageTypeHandshakeInit, [16]byte{}, 0, flight1, make([]byte, AEADTagSize), nil)
	_ = clientTransport.WriteFrame(ctx, frame1)

	frame2, err := clientTransport.ReadFrame(ctx)
	if err != nil {
		t.Fatalf("server dropped connection early: %v", err)
	}
	_ = initiator.ReadFlight(frame2.Payload)

	flight3, _ := initiator.WriteFlight()
	frame3, _ := NewFrame(MessageTypeHandshakeResponse, [16]byte{}, 0, flight3, make([]byte, AEADTagSize), nil)
	_ = clientTransport.WriteFrame(ctx, frame3)

	_, err = clientTransport.ReadFrame(ctx)
	if err == nil {
		t.Fatal("expected connection to be closed by server for unauthorized client")
	}
}

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
