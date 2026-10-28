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
	if err := pinnedExchange(false); err != nil {
		fmt.Println("exchange failed")
		return
	}
	fmt.Println("authenticated application envelope exchanged")
	// Output: authenticated application envelope exchanged
}

// ExampleDialAndStartContext uses the same bounded pinned exchange, with handlers
// installed in config before runtime start and independently canceled establishment.
func ExampleDialAndStartContext() {
	if err := pinnedExchange(true); err != nil {
		fmt.Println("exchange failed")
		return
	}
	fmt.Println("authenticated application envelope exchanged")
	// Output: authenticated application envelope exchanged
}

func pinnedExchange(start bool) (err error) {
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
				if err := validatePinnedEnvelope(data, 1); err != nil {
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
		return errors.Join(err, server.Close())
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	defer func() {
		// All hooks are bounded; Close itself has no overall context deadline.
		err = errors.Join(err, server.Close())
		if e := listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, e)
		}
		join, stopJoin := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopJoin()
		select {
		case e := <-serveDone:
			if e != nil && !errors.Is(e, dgproto.ErrServerClosed) {
				err = errors.Join(err, e)
			}
		case <-join.Done():
			err = errors.Join(err, join.Err())
		}
	}()
	establishment, stop := context.WithTimeout(runtime, time.Second)
	config := dgproto.ClientConfig{
		StaticKey: clientKey, ServerStatic: serverKey.Public(),
		ConnectionConfig: dgproto.ConnectionConfig{WriteTimeout: time.Second, HandlerShutdownTimeout: time.Second,
			Handler: func(ctx context.Context, _ *dgproto.Connection, message any) error {
				if data, ok := message.(*dgproto.EncryptedData); ok {
					if err := validatePinnedEnvelope(data, 2); err != nil {
						return err
					}
					select {
					case received <- struct{}{}:
					case <-ctx.Done():
						return ctx.Err()
					default:
						return errors.New("duplicate demo reply")
					}
				}
				return nil
			},
		},
	}
	var c *dgproto.Connection
	if start {
		c, _, err = dgproto.DialAndStartContext(establishment, runtime, listener.Addr().String(), config)
	} else {
		c, _, err = dgproto.DialContext(establishment, listener.Addr().String(), config)
	}
	stop() // Does not end the returned connection's lifetime.
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, c.Close())
		join, stopJoin := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopJoin()
		select {
		case <-c.Done():
		case <-join.Done():
			err = errors.Join(err, join.Err())
		}
	}()
	if !start {
		c.Start(runtime)
	}
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
	case <-c.Done():
		select {
		case <-received:
			return nil
		default:
		}
		if err := c.Err(); err != nil {
			return err
		}
		return dgproto.ErrConnectionClosed
	}
}

// This example intentionally uses an empty envelope: reject extra fields too.
func validatePinnedEnvelope(data *dgproto.EncryptedData, operation uint8) error {
	if data == nil || data.StreamID != 1 || data.AppMessageType != operation || len(data.Fields) != 0 {
		return errors.New("invalid demo envelope")
	}
	return nil
}
