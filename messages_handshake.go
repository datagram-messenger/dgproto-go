package dgproto

import "fmt"

// NoisePattern identifies a Noise handshake pattern in the wire wrapper.
// DGProto v1 MVP permits only NoisePatternXX.
type NoisePattern uint8

const (
	// NoisePatternXX identifies the Noise XX pattern used by DGProto v1 MVP.
	NoisePatternXX NoisePattern = 1
	// NoisePatternIK is retained for source compatibility with historical code.
	// DGProto v1 MVP serializers and parsers reject it.
	NoisePatternIK NoisePattern = 2
)

// HandshakeInit is the explicitly defined outer wrapper around Noise message 1.
type HandshakeInit struct {
	// Pattern identifies the wrapped Noise handshake pattern.
	Pattern NoisePattern
	// ClientEphemeral is the 32-byte client ephemeral public key.
	ClientEphemeral [32]byte
	// NoisePayload contains Noise handshake bytes.
	NoisePayload []byte
}

// MarshalBinary encodes m as a handshake-init payload and returns owned storage.
func (m HandshakeInit) MarshalBinary() ([]byte, error) {
	if m.Pattern != NoisePatternXX {
		return nil, fmt.Errorf("%w: DGProto v1 MVP requires Noise XX, got 0x%02x", ErrInvalidNoisePattern, m.Pattern)
	}
	if len(m.NoisePayload) != 0 {
		return nil, ErrUnexpectedNoiseData
	}
	total := HandshakeInitFixedSize + len(m.NoisePayload)
	if total > MaxHandshakePayloadSize {
		return nil, fmt.Errorf("%w: got %d, limit %d", ErrMessageLength, total, MaxHandshakePayloadSize)
	}
	if total%4 != 0 {
		return nil, ErrHandshakeAlignment
	}
	buf := make([]byte, total)
	buf[0] = byte(m.Pattern)
	copy(buf[4:36], m.ClientEphemeral[:])
	copy(buf[36:], m.NoisePayload)
	return buf, nil
}

// UnmarshalBinary decodes a handshake-init payload and copies NoisePayload.
func (m *HandshakeInit) UnmarshalBinary(data []byte) error {
	if len(data) < HandshakeInitFixedSize {
		return fmt.Errorf("%w: got %d, want at least %d", ErrMessageTooShort, len(data), HandshakeInitFixedSize)
	}
	if len(data) > MaxHandshakePayloadSize {
		return fmt.Errorf("%w: got %d, limit %d", ErrMessageLength, len(data), MaxHandshakePayloadSize)
	}
	if len(data)%4 != 0 {
		return ErrHandshakeAlignment
	}
	pattern := NoisePattern(data[0])
	if pattern != NoisePatternXX {
		return fmt.Errorf("%w: DGProto v1 MVP requires Noise XX, got 0x%02x", ErrInvalidNoisePattern, pattern)
	}
	if data[1] != 0 || data[2] != 0 || data[3] != 0 {
		return ErrMessageReserved
	}
	if len(data) != HandshakeInitFixedSize {
		return ErrUnexpectedNoiseData
	}
	var ephemeral [32]byte
	copy(ephemeral[:], data[4:36])
	*m = HandshakeInit{Pattern: pattern, ClientEphemeral: ephemeral, NoisePayload: append([]byte(nil), data[36:]...)}
	return nil
}

// HandshakeResponse is the defined outer wrapper around the server Noise flight.
type HandshakeResponse struct {
	// ServerEphemeral is the 32-byte server ephemeral public key.
	ServerEphemeral [32]byte
	// NoisePayload contains Noise handshake bytes.
	NoisePayload []byte
}

// MarshalBinary encodes the exact 96-byte MVP handshake response and returns owned storage.
func (m HandshakeResponse) MarshalBinary() ([]byte, error) {
	total := HandshakeResponseFixedSize + len(m.NoisePayload)
	if total != HandshakeResponseFixedSize+64 {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrMessageLength, total, HandshakeResponseFixedSize+64)
	}
	buf := make([]byte, total)
	copy(buf[:32], m.ServerEphemeral[:])
	copy(buf[32:], m.NoisePayload)
	return buf, nil
}

// UnmarshalBinary decodes exactly one 96-byte MVP handshake response and copies NoisePayload.
func (m *HandshakeResponse) UnmarshalBinary(data []byte) error {
	if len(data) != HandshakeResponseFixedSize+64 {
		return fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(data), HandshakeResponseFixedSize+64)
	}
	var ephemeral [32]byte
	copy(ephemeral[:], data[:32])
	*m = HandshakeResponse{ServerEphemeral: ephemeral, NoisePayload: append([]byte(nil), data[32:]...)}
	return nil
}

// HandshakeFinish carries the third and final Noise XX flight.
type HandshakeFinish struct {
	// NoisePayload is the fixed 64-byte final Noise XX flight.
	NoisePayload []byte
}

// MarshalBinary validates and copies the fixed-size final Noise XX flight.
func (m HandshakeFinish) MarshalBinary() ([]byte, error) {
	if len(m.NoisePayload) != HandshakeFinishFixedSize {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(m.NoisePayload), HandshakeFinishFixedSize)
	}
	return append([]byte(nil), m.NoisePayload...), nil
}

// UnmarshalBinary decodes and copies the fixed-size final Noise XX flight.
func (m *HandshakeFinish) UnmarshalBinary(data []byte) error {
	if len(data) != HandshakeFinishFixedSize {
		return fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(data), HandshakeFinishFixedSize)
	}
	*m = HandshakeFinish{NoisePayload: append([]byte(nil), data...)}
	return nil
}
