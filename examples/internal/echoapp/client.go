package echoapp

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Exchange owns one bounded request and its stopped, dialed connection.
// Success requires a callback-published, validated reply, not just a local write.
// Demo servers remain open until the client closes; immediate peer shutdown can
// discard queued callbacks. Identity and pin provisioning belong to the caller.
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
				validation := Validate(data, replyType)
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
		err = joinClientClose(err, c.Close())
		joinCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		select {
		case <-c.Done():
		case <-joinCtx.Done():
			err = errors.Join(err, joinCtx.Err())
		}
	}()
	c.Start(runtimeCtx) // Install handler before starting the caller-owned runtime.
	request, err := dgproto.NewEncryptedData(streamID, requestType, dgproto.TLV{Type: payloadField, Value: []byte(Payload)})
	if err != nil {
		return err
	}
	if err := request.ValidateFrameSize(padLength); err != nil {
		return err
	}
	writeCtx, stopWrite := context.WithTimeout(runtimeCtx, operationTimeout)
	err = c.SendPaddedAndWait(writeCtx, request, padLength)
	stopWrite()
	if err != nil {
		return err
	}
	// Keep the request immutable, including after cancellation of write-wait.
	select {
	case err := <-received:
		return err
	case <-runtimeCtx.Done():
		return runtimeCtx.Err()
	case <-c.Done():
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

func joinClientClose(operationErr, closeErr error) error {
	// Only exact benign sentinels after a validated reply are suppressed.
	// Wrapping/joining can carry additional information and must be retained.
	if operationErr == nil && (closeErr == io.EOF || closeErr == net.ErrClosed || closeErr == dgproto.ErrConnectionClosed) {
		return nil
	}
	return errors.Join(operationErr, closeErr)
}
