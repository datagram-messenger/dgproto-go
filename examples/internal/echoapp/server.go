package echoapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Reply validates the request and owns a fresh immutable reply buffer.
// Stay open after the local write: only the client knows when it has validated
// the reply. Closing here could discard its queued callback before receipt.
func Reply(ctx context.Context, c *dgproto.Connection, message any) error {
	data, ok := message.(*dgproto.EncryptedData)
	if !ok {
		return nil
	} // Protocol control messages belong to the runtime.
	if err := Validate(data, requestType); err != nil {
		return err
	}
	reply, err := dgproto.NewEncryptedData(streamID, replyType, data.Fields...)
	if err != nil {
		return err
	}
	if err := reply.ValidateFrameSize(padLength); err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	// Never mutate the reply, including after a canceled write-wait.
	return c.SendPaddedAndWait(writeCtx, reply, padLength)
}

// NewServer requires an explicit single-client allowlist. The returned server is
// caller-owned; Serve and Close remain visible in the server command.
func NewServer(key dgproto.StaticKey, clientPublic []byte) (*dgproto.Server, error) {
	if len(clientPublic) != 32 || bytes.Equal(clientPublic, make([]byte, 32)) {
		return nil, ErrIdentityInput
	}
	return dgproto.NewServer(dgproto.ServerConfig{
		StaticKey: key, AllowedClients: [][]byte{bytes.Clone(clientPublic)},
		MaxConcurrentHandshakes: 4, MaxActiveConnections: 8,
		OutboundQueue: 4, HandlerQueue: 4, IdleTimeout: 15 * time.Second,
		HandshakeTimeout: 2 * time.Second, WriteTimeout: operationTimeout,
		HandlerShutdownTimeout: operationTimeout, Handler: Reply,
	})
}

// Serve owns the listener from entry, including pre-canceled contexts.
// It joins Serve and bounded server callbacks before returning. The supplied
// server is single-use; this demo does not install unbounded hooks.
func Serve(ctx context.Context, server *dgproto.Server, listener net.Listener) (err error) {
	defer func() {
		if e := server.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, e)
		}
		if e := listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, e)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case e := <-done:
		if !errors.Is(e, dgproto.ErrServerClosed) {
			return e
		}
		return nil
	case <-ctx.Done():
		// Close joins callbacks; these callbacks only perform bounded writes.
		if e := server.Close(); e != nil {
			err = errors.Join(err, e)
		}
		if e := listener.Close(); e != nil && !errors.Is(e, net.ErrClosed) {
			err = errors.Join(err, e)
		}
		joinCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case e := <-done:
			if !errors.Is(e, dgproto.ErrServerClosed) {
				err = errors.Join(err, e)
			}
		case <-joinCtx.Done():
			err = errors.Join(err, joinCtx.Err())
		}
		return err // Signal shutdown is normal; caller can inspect its context.
	}
}
