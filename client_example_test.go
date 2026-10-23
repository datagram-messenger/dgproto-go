package dgproto_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// ExampleDialContext is a loopback application-envelope exchange, not a messaging
// application server. Demo identities exist only in memory. Real applications
// must provision/persist/protect identities and authorize accounts and actions;
// Noise possession and an allowlist are not an account system or durable delivery.
func ExampleDialContext() {
	if err := pinnedExchange(); err != nil {
		fmt.Println("exchange failed")
		return
	}
	fmt.Println("authenticated application envelope exchanged")
	// Output: authenticated application envelope exchanged
}

func pinnedExchange() error {
	serverKey, err := dgproto.GenerateStaticKey()
	if err != nil {
		return err
	}
	clientKey, err := dgproto.GenerateStaticKey()
	if err != nil {
		return err
	}
	runtime, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received := make(chan struct{}, 1)
	server, err := dgproto.NewServer(dgproto.ServerConfig{
		StaticKey: serverKey, AllowedClients: [][]byte{clientKey.Public()},
		HandshakeTimeout: time.Second, WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second,
		Handler: func(ctx context.Context, c *dgproto.Connection, message any) error {
			if data, ok := message.(*dgproto.EncryptedData); ok {
				// This demo echoes a fixed kind. Production must validate payloads and
				// authorize each operation before dispatching bounded downstream work.
				if data.StreamID != 1 || data.AppMessageType != 1 {
					return errors.New("unsupported demo operation")
				}
				writeCtx, stop := context.WithTimeout(ctx, time.Second)
				defer stop()
				return c.SendPaddedAndWait(writeCtx, dgproto.EncryptedData{StreamID: 1, AppMessageType: 2}, 16)
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		_ = server.Abort()
		_ = server.Close()
		select {
		case <-serveDone:
		case <-runtime.Done():
		}
	}()
	establishment, stop := context.WithTimeout(runtime, time.Second)
	c, _, err := dgproto.DialContext(establishment, listener.Addr().String(), dgproto.ClientConfig{
		StaticKey: clientKey, ServerStatic: serverKey.Public(),
		ConnectionConfig: dgproto.ConnectionConfig{WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second,
			Handler: func(ctx context.Context, _ *dgproto.Connection, message any) error {
				if data, ok := message.(*dgproto.EncryptedData); ok && data.AppMessageType == 2 {
					select {
					case received <- struct{}{}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			},
		},
	})
	stop() // Does not end the returned connection's lifetime.
	if err != nil {
		return err
	}
	defer c.Close()
	c.Start(runtime)
	writeCtx, stopWrite := context.WithTimeout(runtime, time.Second)
	defer stopWrite()
	if err = c.SendPaddedAndWait(writeCtx, dgproto.EncryptedData{StreamID: 1, AppMessageType: 1}, 16); err != nil {
		return err
	}
	select {
	case <-received:
		return nil
	case <-runtime.Done():
		return runtime.Err()
	}
}
