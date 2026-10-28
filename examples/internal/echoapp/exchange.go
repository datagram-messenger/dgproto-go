package echoapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

const (
	Payload          = "hello DGProto"
	operationTimeout = time.Second
)

var ErrEnvelope = errors.New("echo: invalid application envelope")

func Validate(data *dgproto.EncryptedData, operation uint8) error {
	if data == nil || data.StreamID != 1 || data.AppMessageType != operation ||
		len(data.Fields) != 1 || data.Fields[0].Type != 1 || !bytes.Equal(data.Fields[0].Value, []byte(Payload)) {
		return ErrEnvelope
	}
	return nil
}

// Reply validates the demo request and owns a fresh immutable reply buffer.
// The server stays open after the local write: the client initiates close only
// after its reply callback publishes the result. Do not append an immediate
// server Close here; local write completion is not client callback completion.
func Reply(ctx context.Context, c *dgproto.Connection, message any) error {
	data, ok := message.(*dgproto.EncryptedData)
	if !ok {
		return nil // Protocol control messages belong to the runtime.
	}
	if err := Validate(data, 1); err != nil {
		return err
	}
	reply, err := dgproto.NewEncryptedData(1, 2, data.Fields...)
	if err != nil {
		return err
	}
	if err := reply.ValidateFrameSize(16); err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	// Never mutate the reply, including after a canceled write-wait.
	return c.SendPaddedAndWait(writeCtx, reply, 16)
}

// NewServer requires an explicit single-client allowlist. The returned server is
// caller-owned; Serve and Close are intentionally visible in the server command.
func NewServer(key dgproto.StaticKey, clientPublic []byte) (*dgproto.Server, error) {
	if len(clientPublic) != 32 || bytes.Equal(clientPublic, make([]byte, 32)) {
		return nil, ErrIdentityInput // Never accidentally enable an empty allowlist.
	}
	return dgproto.NewServer(dgproto.ServerConfig{
		StaticKey: key, AllowedClients: [][]byte{bytes.Clone(clientPublic)},
		MaxConcurrentHandshakes: 4, MaxActiveConnections: 8,
		HandshakeTimeout: 2 * time.Second, WriteTimeout: operationTimeout,
		HandlerShutdownTimeout: operationTimeout, Handler: Reply,
	})
}

// Exchange is one bounded demo request, not a persistent client API. It owns and
// closes its dialed connection and joins Done even when the caller cancels.
// The caller owns identity/pin provisioning. No retries or TOFU are performed.
// Success requires a validated callback-published reply. With an arbitrary peer
// that closes immediately after writing, shutdown may discard the queued reply
// before its callback begins, so Exchange can return a terminal error instead.
// Demo peers coordinate by leaving the server open until the client closes.
func Exchange(ctx context.Context, address string, key dgproto.StaticKey, pin []byte) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtimeCtx, cancelRuntime := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRuntime()
	received := make(chan error, 1)
	establishmentCtx, stopEstablishment := context.WithTimeout(runtimeCtx, 2*time.Second)
	c, _, err := dgproto.DialContext(establishmentCtx, address, dgproto.ClientConfig{
		StaticKey: key, ServerStatic: pin,
		ConnectionConfig: dgproto.ConnectionConfig{
			HandshakeTimeout: 2 * time.Second, WriteTimeout: operationTimeout,
			HandlerShutdownTimeout: operationTimeout,
			Handler: func(ctx context.Context, _ *dgproto.Connection, message any) error {
				data, ok := message.(*dgproto.EncryptedData)
				if !ok {
					return nil
				}
				validation := Validate(data, 2)
				select {
				case received <- validation:
					return validation
				case <-ctx.Done():
					return ctx.Err()
				default:
					return ErrEnvelope
				}
			},
		},
	})
	stopEstablishment() // Establishment cancellation does not own runtime lifetime.
	if err != nil {
		return err
	}
	defer func() {
		closeErr := c.Close()
		// A validated reply is the operation result. The peer may close before
		// our close-frame write completes; EOF/closed in that cleanup race does
		// not undo receipt. Without a validated reply, retain every failure.
		normalAfterReply := err == nil && (errors.Is(closeErr, io.EOF) ||
			errors.Is(closeErr, net.ErrClosed) || errors.Is(closeErr, dgproto.ErrConnectionClosed))
		if closeErr != nil && !normalAfterReply {
			err = errors.Join(err, closeErr)
		}
		joinCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-c.Done():
		case <-joinCtx.Done():
			err = errors.Join(err, joinCtx.Err())
		}
	}()
	c.Start(runtimeCtx) // Stopped, caller-owned runtime; handler installed first.
	request, err := dgproto.NewEncryptedData(1, 1, dgproto.TLV{Type: 1, Value: []byte(Payload)})
	if err != nil {
		return err
	}
	if err := request.ValidateFrameSize(16); err != nil {
		return err
	}
	writeCtx, stopWrite := context.WithTimeout(runtimeCtx, operationTimeout)
	err = c.SendPaddedAndWait(writeCtx, request, 16)
	stopWrite()
	if err != nil {
		return err
	}
	// Immutable request survives cancellation; local write is not peer receipt.
	select {
	case err := <-received:
		return err
	case <-runtimeCtx.Done():
		return runtimeCtx.Err()
	case <-c.Done():
		// A queued reply may race EOF; prefer the validated application result.
		select {
		case err := <-received:
			return err
		default:
		}
		if err := c.Err(); err != nil {
			return err
		}
		return dgproto.ErrConnectionClosed
	}
}

// Serve owns the supplied listener from entry, including pre-canceled contexts.
// It joins Serve and bounded server callbacks before returning. The server is
// single-use and caller-provided; there are no unbounded hooks in this demo.
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
		return err // Signal shutdown is normal; callers can inspect their context.
	}
}
