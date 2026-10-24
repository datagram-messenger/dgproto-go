package dgproto

import (
	"context"
	"fmt"
)

func (s *Server) performHandshake(ctx context.Context, transport *TCPTransport) (*Session, HandshakeResult, error) {
	responder, err := NewResponderHandshake(s.config.StaticKey, nil)
	if err != nil {
		return nil, HandshakeResult{}, err
	}

	frame1, err := transport.ReadFrame(ctx)
	if err != nil {
		return nil, HandshakeResult{}, err
	}
	if err := validateHandshakeFrame(frame1, MessageTypeHandshakeInit); err != nil {
		return nil, HandshakeResult{}, err
	}
	if err := responder.ReadFlight(frame1.Payload); err != nil {
		return nil, HandshakeResult{}, err
	}

	flight2Payload, err := responder.WriteFlight()
	if err != nil {
		return nil, HandshakeResult{}, err
	}
	frame2, err := NewFrame(MessageTypeHandshakeResponse, [16]byte{}, 0, flight2Payload, make([]byte, AEADTagSize), nil)
	if err != nil {
		return nil, HandshakeResult{}, err
	}
	if err := transport.WriteFrame(ctx, frame2); err != nil {
		return nil, HandshakeResult{}, err
	}

	frame3, err := transport.ReadFrame(ctx)
	if err != nil {
		return nil, HandshakeResult{}, err
	}
	if err := validateHandshakeFrame(frame3, MessageTypeHandshakeResponse); err != nil {
		return nil, HandshakeResult{}, err
	}
	if err := responder.ReadFlight(frame3.Payload); err != nil {
		return nil, HandshakeResult{}, err
	}

	if !responder.Complete() {
		return nil, HandshakeResult{}, ErrHandshakeFailed
	}

	result, err := responder.Result()
	if err != nil {
		return nil, HandshakeResult{}, err
	}

	if len(s.config.AllowedClients) > 0 {
		if !isClientAllowed(result.PeerStatic, s.config.AllowedClients) {
			return nil, HandshakeResult{}, ErrClientNotPermitted
		}
	}

	session, err := NewSessionFromHandshake(s.config.CipherSuite, result)
	return session, result, err
}

func validateHandshakeFrame(frame Frame, expected MessageType) error {
	if frame.Header.MessageType != expected {
		return fmt.Errorf("%w: expected handshake type 0x%02x, got 0x%02x", ErrHandshakeFailed, expected, frame.Header.MessageType)
	}
	if frame.Header.SessionID != ([16]byte{}) {
		return fmt.Errorf("%w: handshake session ID must be zero", ErrHandshakeFailed)
	}
	if frame.Header.Sequence != 0 {
		return fmt.Errorf("%w: handshake sequence must be zero", ErrHandshakeFailed)
	}
	if err := frame.ValidateReceive(); err != nil {
		return fmt.Errorf("%w: %v", ErrHandshakeFailed, err)
	}
	return nil
}

func isClientAllowed(clientStatic [32]byte, allowedList [][]byte) bool {
	for _, allowed := range allowedList {
		if len(allowed) == 32 && hmacEqual(clientStatic[:], allowed) {
			return true
		}
	}
	return false
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}
