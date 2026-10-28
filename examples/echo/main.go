// Command echo runs one pinned, allowlisted application echo on loopback.
// Its ephemeral identities are for learning, not production key management.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

const (
	demoPayload            = "hello DGProto"
	successOutput          = "authenticated application echo received\n"
	padLength        uint8 = 16
	operationTimeout       = time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Stdout); err != nil {
		// Do not print peer metadata, payloads, identities or key material.
		fmt.Fprintln(os.Stderr, "echo failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, out io.Writer) error {
	if err := exchange(ctx); err != nil {
		return err
	}
	_, err := io.WriteString(out, successOutput)
	return err
}

func exchange(ctx context.Context) (err error) {
	if err := ctx.Err(); err != nil {
		return err // No keys, sockets or goroutines for an already canceled run.
	}
	serverKey, err := dgproto.GenerateStaticKey()
	if err != nil {
		return err
	}
	clientKey, err := dgproto.GenerateStaticKey()
	if err != nil {
		return err
	}
	runtimeCtx, cancelRuntime := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRuntime()

	server, err := dgproto.NewServer(dgproto.ServerConfig{
		StaticKey:               serverKey,
		AllowedClients:          [][]byte{clientKey.Public()},
		MaxConcurrentHandshakes: 1,
		MaxActiveConnections:    1,
		HandshakeTimeout:        2 * time.Second,
		WriteTimeout:            operationTimeout,
		HandlerShutdownTimeout:  operationTimeout,
		Handler: func(ctx context.Context, c *dgproto.Connection, message any) error {
			data, ok := message.(*dgproto.EncryptedData)
			if !ok {
				return nil // The runtime owns protocol control messages.
			}
			if err := validateEcho(data, 1); err != nil {
				return err
			}
			// Own the reply buffers; never mutate them, even if write-wait is canceled.
			reply := dgproto.EncryptedData{
				StreamID: data.StreamID, AppMessageType: 2,
				Fields: []dgproto.TLV{{Type: 1, Value: append([]byte(nil), data.Fields[0].Value...)}},
			}
			if err := reply.ValidateFrameSize(padLength); err != nil {
				return err
			}
			writeCtx, stop := context.WithTimeout(ctx, operationTimeout)
			defer stop()
			return c.SendPaddedAndWait(writeCtx, reply, padLength)
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
		// Call Close to join server-owned work. All demo callbacks are bounded.
		err = errors.Join(err, server.Close())
		// Also own the listener if shutdown raced Serve's registration.
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
		joinCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case serveErr := <-serveDone:
			if serveErr != nil && !errors.Is(serveErr, dgproto.ErrServerClosed) {
				err = errors.Join(err, serveErr)
			}
		case <-joinCtx.Done():
			err = errors.Join(err, joinCtx.Err())
		}
	}()

	received := make(chan error, 1)
	establishmentCtx, stopEstablishment := context.WithTimeout(runtimeCtx, 2*time.Second)
	c, _, err := dgproto.DialContext(establishmentCtx, listener.Addr().String(), dgproto.ClientConfig{
		StaticKey:    clientKey,
		ServerStatic: serverKey.Public(), // Trusted directly from our local demo server, not TOFU.
		ConnectionConfig: dgproto.ConnectionConfig{
			HandshakeTimeout:       2 * time.Second,
			WriteTimeout:           operationTimeout,
			HandlerShutdownTimeout: operationTimeout,
			Handler: func(ctx context.Context, _ *dgproto.Connection, message any) error {
				data, ok := message.(*dgproto.EncryptedData)
				if !ok {
					return nil
				}
				validationErr := validateEcho(data, 2)
				select {
				case received <- validationErr:
					return validationErr
				case <-ctx.Done():
					return ctx.Err()
				default:
					return errors.New("duplicate demo echo")
				}
			},
		},
	})
	stopEstablishment() // Independent of the returned connection's runtime.
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, c.Close()) // Always close, even before Start.
		joinCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-c.Done():
		case <-joinCtx.Done():
			err = errors.Join(err, joinCtx.Err())
		}
	}()
	c.Start(runtimeCtx) // DialContext returns a stopped, caller-owned Connection.

	request := dgproto.EncryptedData{
		StreamID: 1, AppMessageType: 1,
		Fields: []dgproto.TLV{{Type: 1, Value: []byte(demoPayload)}},
	}
	if err := request.ValidateFrameSize(padLength); err != nil {
		return err
	}
	writeCtx, stopWrite := context.WithTimeout(runtimeCtx, operationTimeout)
	err = c.SendPaddedAndWait(writeCtx, request, padLength)
	stopWrite()
	// The request and its TLV buffers stay immutable through shutdown, including
	// cancellation after enqueue. Local write completion is not peer receipt.
	if err != nil {
		return err
	}
	select {
	case err := <-received:
		return err // Only a validated application reply establishes demo success.
	case <-runtimeCtx.Done():
		return runtimeCtx.Err()
	case <-c.Done():
		return c.Err()
	}
}

func validateEcho(data *dgproto.EncryptedData, messageType uint8) error {
	if data == nil || data.StreamID != 1 || data.AppMessageType != messageType ||
		len(data.Fields) != 1 || data.Fields[0].Type != 1 || string(data.Fields[0].Value) != demoPayload {
		return errors.New("invalid demo application envelope")
	}
	return nil
}
