package dgproto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"

	"github.com/flynn/noise"
)

var (
	// ErrHandshake indicates a failed or out-of-order Noise handshake operation.
	ErrHandshake = errors.New("dgproto: handshake failed")
	noiseSuite   = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)
)

// HandshakeResult contains the authenticated identity and session material.
type HandshakeResult struct {
	SessionID  [16]byte
	SendKey    [KeySize]byte
	ReceiveKey [KeySize]byte
	PeerStatic [noiseKeySize]byte
}

type handshakeRole uint8
type handshakeStep uint8

const (
	roleInitiator handshakeRole = iota + 1
	roleResponder
)

const (
	stepInitiatorWrite1 handshakeStep = iota + 1
	stepInitiatorRead2
	stepInitiatorWrite3
	stepResponderRead1
	stepResponderWrite2
	stepResponderRead3
	stepComplete
	stepFailed
)

// Handshake executes one side of the strict-MVP
// Noise_XX_25519_ChaChaPoly_SHA256 exchange. A Handshake must be driven in
// flight order and is not safe for concurrent use.
type Handshake struct {
	state        *noise.HandshakeState
	role         handshakeRole
	step         handshakeStep
	expectedPeer []byte
	result       *HandshakeResult
}

// NewInitiatorHandshake creates the client-side three-flight state machine.
func NewInitiatorHandshake(static StaticKey, expectedResponderStatic []byte) (*Handshake, error) {
	return newHandshake(roleInitiator, static, expectedResponderStatic, rand.Reader, []byte("DGPv1"))
}

// NewResponderHandshake creates the server-side three-flight state machine.
func NewResponderHandshake(static StaticKey, expectedInitiatorStatic []byte) (*Handshake, error) {
	return newHandshake(roleResponder, static, expectedInitiatorStatic, rand.Reader, []byte("DGPv1"))
}

func newHandshake(role handshakeRole, static StaticKey, expectedPeer []byte, random io.Reader, prologue []byte) (*Handshake, error) {
	if err := static.validate(); err != nil {
		return nil, err
	}
	if len(expectedPeer) != 0 && len(expectedPeer) != noiseKeySize {
		return nil, ErrHandshake
	}
	state, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   noiseSuite,
		Random:        random,
		Pattern:       noise.HandshakeXX,
		Initiator:     role == roleInitiator,
		Prologue:      append([]byte(nil), prologue...),
		StaticKeypair: static.noiseKey(),
	})
	if err != nil {
		return nil, ErrHandshake
	}
	step := stepResponderRead1
	if role == roleInitiator {
		step = stepInitiatorWrite1
	}
	return &Handshake{state: state, role: role, step: step, expectedPeer: append([]byte(nil), expectedPeer...)}, nil
}

// WriteFlight emits the next flight owned by the caller.
func (h *Handshake) WriteFlight() ([]byte, error) {
	if h == nil || h.state == nil {
		return nil, ErrHandshake
	}
	switch h.step {
	case stepInitiatorWrite1:
		message, _, _, err := h.state.WriteMessage(nil, nil)
		if err != nil || len(message) != 32 {
			return nil, h.fail()
		}
		var ephemeral [32]byte
		copy(ephemeral[:], message)
		wire, err := (HandshakeInit{Pattern: NoisePatternXX, ClientEphemeral: ephemeral}).MarshalBinary()
		if err != nil {
			return nil, h.fail()
		}
		h.step = stepInitiatorRead2
		return wire, nil
	case stepResponderWrite2:
		message, _, _, err := h.state.WriteMessage(nil, nil)
		if err != nil || len(message) != 96 {
			return nil, h.fail()
		}
		var ephemeral [32]byte
		copy(ephemeral[:], message[:32])
		wire, err := (HandshakeResponse{ServerEphemeral: ephemeral, NoisePayload: message[32:]}).MarshalBinary()
		if err != nil {
			return nil, h.fail()
		}
		h.step = stepResponderRead3
		return wire, nil
	case stepInitiatorWrite3:
		message, send, receive, err := h.state.WriteMessage(nil, nil)
		if err != nil || len(message) != 64 || send == nil || receive == nil {
			return nil, h.fail()
		}
		wire, err := (HandshakeFinish{NoisePayload: message}).MarshalBinary()
		if err != nil || h.complete(send, receive) != nil {
			return nil, h.fail()
		}
		return wire, nil
	default:
		return nil, ErrHandshake
	}
}

// ReadFlight consumes the next peer flight and copies all input data.
func (h *Handshake) ReadFlight(wire []byte) error {
	if h == nil || h.state == nil {
		return ErrHandshake
	}
	var message []byte
	switch h.step {
	case stepResponderRead1:
		var flight HandshakeInit
		if err := flight.UnmarshalBinary(wire); err != nil || flight.Pattern != NoisePatternXX {
			return h.fail()
		}
		message = append(message, flight.ClientEphemeral[:]...)
	case stepInitiatorRead2:
		var flight HandshakeResponse
		if err := flight.UnmarshalBinary(wire); err != nil || len(flight.NoisePayload) != 64 {
			return h.fail()
		}
		message = append(message, flight.ServerEphemeral[:]...)
		message = append(message, flight.NoisePayload...)
	case stepResponderRead3:
		var flight HandshakeFinish
		if err := flight.UnmarshalBinary(wire); err != nil || len(flight.NoisePayload) != 64 {
			return h.fail()
		}
		message = append(message, flight.NoisePayload...)
	default:
		return ErrHandshake
	}

	_, send, receive, err := h.state.ReadMessage(nil, message)
	if err != nil {
		return h.fail()
	}
	switch h.step {
	case stepResponderRead1:
		h.step = stepResponderWrite2
	case stepInitiatorRead2:
		h.step = stepInitiatorWrite3
	case stepResponderRead3:
		if send == nil || receive == nil || h.complete(send, receive) != nil {
			return h.fail()
		}
	}
	return nil
}

// Complete reports whether final session material is available.
func (h *Handshake) Complete() bool { return h != nil && h.step == stepComplete }

// Result returns an owned copy after successful completion.
func (h *Handshake) Result() (HandshakeResult, error) {
	if !h.Complete() || h.result == nil {
		return HandshakeResult{}, ErrHandshake
	}
	return *h.result, nil
}

func (h *Handshake) complete(send, receive *noise.CipherState) error {
	peer := h.state.PeerStatic()
	if len(peer) != noiseKeySize || (len(h.expectedPeer) != 0 && subtle.ConstantTimeCompare(peer, h.expectedPeer) != 1) {
		return ErrHandshake
	}
	binding := h.state.ChannelBinding()
	if len(binding) == 0 {
		return ErrHandshake
	}
	first, second := send.UnsafeKey(), receive.UnsafeKey()
	result := HandshakeResult{SessionID: transcriptSessionID(binding)}
	copy(result.PeerStatic[:], peer)
	if h.role == roleInitiator {
		copy(result.SendKey[:], first[:])
		copy(result.ReceiveKey[:], second[:])
	} else {
		copy(result.SendKey[:], second[:])
		copy(result.ReceiveKey[:], first[:])
	}
	h.result = &result
	h.step = stepComplete
	h.state = nil
	return nil
}

func transcriptSessionID(binding []byte) [16]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("DGPv1 SessionID"))
	_, _ = hash.Write(binding)
	var id [16]byte
	copy(id[:], hash.Sum(nil)[:16])
	return id
}

func (h *Handshake) fail() error {
	h.step, h.state, h.result = stepFailed, nil, nil
	return ErrHandshake
}

var _ = fmt.Sprintf
