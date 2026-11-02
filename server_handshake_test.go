package dgproto

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

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

// Exercise the actual Noise authorization boundary, not just stored slice equality.
func checkServerAllowlist(t *testing.T, server *Server, key StaticKey, want error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, peer := net.Pipe()
	defer client.Close()
	defer peer.Close()
	result := make(chan error, 1)
	go func() {
		session, _, err := server.performHandshake(ctx, NewTCPTransport(peer))
		if session != nil {
			_ = session.Close()
		}
		result <- err
	}()
	_, err := establishClient(ctx, NewTCPTransport(client), ClientConfig{
		StaticKey: key, ServerStatic: server.config.StaticKey.Public(),
	})
	if err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("server authorization = %v, want %v", err, want)
		}
	case <-ctx.Done():
		t.Fatal("server handshake did not finish")
	}
}

func TestServerAllowedClientsSnapshot(t *testing.T) {
	for _, mutation := range []string{"outer replacement", "inner mutation"} {
		t.Run(mutation, func(t *testing.T) {
			allowedKey := generateTestStaticKey(t)
			rejectedKey := generateTestStaticKey(t)
			input := [][]byte{allowedKey.Public()}
			server, err := NewServer(ServerConfig{StaticKey: generateTestStaticKey(t), AllowedClients: input})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			if mutation == "outer replacement" {
				input[0] = rejectedKey.Public()
			} else {
				copy(input[0], rejectedKey.Public())
			}
			checkServerAllowlist(t, server, allowedKey, nil)
			checkServerAllowlist(t, server, rejectedKey, ErrClientNotPermitted)
		})
	}
}

func TestServerAllowedClientsConcurrentReuse(t *testing.T) {
	allowedKey := generateTestStaticKey(t)
	rejectedKey := generateTestStaticKey(t)
	input := [][]byte{allowedKey.Public()}
	server, err := NewServer(ServerConfig{StaticKey: generateTestStaticKey(t), AllowedClients: input})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// Start only after construction; all caller writes belong to this goroutine.
	started, stop, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
				input[0][0] ^= 1
				input[0] = append([]byte(nil), input[0]...)
			}
		}
	}()
	defer func() { close(stop); <-done }()
	<-started
	for i := 0; i < 4; i++ {
		checkServerAllowlist(t, server, allowedKey, nil)
		checkServerAllowlist(t, server, rejectedKey, ErrClientNotPermitted)
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
