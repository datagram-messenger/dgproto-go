package dgproto

import (
	"context"
	"fmt"
	"net"
)

// DialContext dials TCP and completes a pinned Noise XX handshake. It returns a
// STOPPED established Connection and non-secret authenticated metadata. Call
// Start(runtimeCtx) explicitly; ctx bounds establishment only, not the returned
// connection's lifetime. The caller owns the connection and must Close it even
// if it never starts it. Success is a complete local final-flight write, not
// server admission, readiness, application authorization, or peer acknowledgement.
//
// ctx must be nonnil. HandshakeTimeout zero selects 10 seconds covering dial and
// all flights. The earlier of that timeout and ctx's deadline wins; cancellation
// interrupts establishment. Invalid configuration is rejected before dialing.
// Failure closes the acquired socket exactly once and returns no connection or
// metadata. Observed cancellation preserves errors.Is with context errors;
// handshake failures wrap ErrHandshakeFailed, and network errors remain available
// through errors.As. No traffic keys are returned.
func DialContext(ctx context.Context, address string, config ClientConfig) (*Connection, AdmissionInfo, error) {
	dialer := &net.Dialer{}
	return dialContext(ctx, address, config, dialer.DialContext)
}

// dialContext's private dial seam keeps cleanup and cancellation tests independent
// of OS-specific connect timing; public callers always use the standard dialer.
func dialContext(ctx context.Context, address string, config ClientConfig, dial func(context.Context, string, string) (net.Conn, error)) (*Connection, AdmissionInfo, error) {
	if ctx == nil {
		return nil, AdmissionInfo{}, fmt.Errorf("%w: nil context", ErrInvalidClientConfig)
	}
	var err error
	config, err = normalizeClientConfig(config)
	if err != nil {
		return nil, AdmissionInfo{}, err
	}
	if err = ctx.Err(); err != nil {
		return nil, AdmissionInfo{}, err
	}
	establishment, cancel := context.WithTimeout(ctx, config.HandshakeTimeout)
	defer cancel()
	raw, err := dial(establishment, "tcp", address)
	if err != nil {
		if raw != nil {
			_ = raw.Close()
		}
		return nil, AdmissionInfo{}, fmt.Errorf("dgproto: dial: %w", transportError(establishment, err))
	}
	if raw == nil {
		return nil, AdmissionInfo{}, fmt.Errorf("dgproto: dial: %w", ErrTransportClosed)
	}
	transport := NewTCPTransport(raw)
	owned := true
	defer func() {
		if owned {
			_ = transport.Close()
		}
	}()
	result, err := establishClient(establishment, transport, config)
	if err != nil {
		return nil, AdmissionInfo{}, err
	}
	session, err := NewSessionFromHandshake(CipherChaCha20Poly1305, result)
	if err != nil {
		return nil, AdmissionInfo{}, err
	}
	// Transport watchers have joined and cleared their deadlines before this check.
	// Cancellation observed here fails closed; after this point ownership transfers.
	if err = establishment.Err(); err != nil {
		_ = session.Close()
		return nil, AdmissionInfo{}, err
	}
	conn := NewConnection(transport, session, config.ConnectionConfig)
	info := AdmissionInfo{PeerStatic: result.PeerStatic, SessionID: result.SessionID, RemoteAddr: raw.RemoteAddr()}
	owned = false
	return conn, info, nil
}

func establishClient(ctx context.Context, t *TCPTransport, config ClientConfig) (HandshakeResult, error) {
	fail := func(err error) (HandshakeResult, error) {
		if ctx.Err() != nil {
			return HandshakeResult{}, ctx.Err()
		}
		return HandshakeResult{}, fmt.Errorf("%w: %w", ErrHandshakeFailed, err)
	}
	h, err := NewInitiatorHandshake(config.StaticKey, config.ServerStatic)
	if err != nil {
		return fail(err)
	}
	flight, err := h.WriteFlight()
	if err != nil {
		return fail(err)
	}
	frame := Frame{Header: NewHeader(MessageTypeHandshakeInit, [16]byte{}, 0, uint32(len(flight)), 0), Payload: flight}
	if err = t.WriteFrame(ctx, frame); err != nil {
		return fail(err)
	}
	response, err := t.ReadFrame(ctx)
	if err != nil {
		return fail(err)
	}
	if err = validateHandshakeFrame(response, MessageTypeHandshakeResponse); err != nil {
		return fail(err)
	}
	if err = h.ReadFlight(response.Payload); err != nil {
		return fail(err)
	}
	// Existing Noise helper checks the pin before releasing the final flight.
	flight, err = h.WriteFlight()
	if err != nil {
		return fail(err)
	}
	frame = Frame{Header: NewHeader(MessageTypeHandshakeResponse, [16]byte{}, 0, uint32(len(flight)), 0), Payload: flight}
	if err = t.WriteFrame(ctx, frame); err != nil {
		return fail(err)
	}
	result, err := h.Result()
	if err != nil {
		return fail(err)
	}
	return result, nil
}
