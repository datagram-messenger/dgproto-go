package dgproto

import (
	"errors"
	"fmt"

	"github.com/datagram-messenger/dgproto-go/internal/wire"
)

const (
	// HandshakeInitFixedSize is the 36-byte Noise XX message-one wrapper size.
	HandshakeInitFixedSize = 36
	// HandshakeResponseFixedSize is the 32-byte server-ephemeral prefix size;
	// the complete MVP response is this prefix followed by exactly 64 Noise bytes.
	HandshakeResponseFixedSize = 32
	// HandshakeFinishFixedSize is the 64-byte Noise XX final-flight size.
	HandshakeFinishFixedSize = 64
	// PingPongSize is the fixed ping or pong payload size.
	PingPongSize = wire.PingPongSize
	// RekeyInitSize is the fixed rekey payload size.
	RekeyInitSize = wire.RekeyInitSize
	// MaxAckSequences is the largest count representable by the Ack wire field.
	MaxAckSequences = wire.MaxAckSequences
	// MaxEncryptedPayloadSize excludes the header and 16-byte AEAD tag.
	MaxEncryptedPayloadSize = MaxFrameSize - HeaderSize - AEADTagSize
	// MaxHandshakePayloadSize excludes the header; handshakes have no outer tag.
	MaxHandshakePayloadSize = MaxFrameSize - HeaderSize
	// MaxResumptionTicketSize is the post-MVP ticket byte limit.
	MaxResumptionTicketSize = MaxEncryptedPayloadSize - 8
	// MaxReasonSize is the largest text value whose aligned TLV and message code
	// fit in the DGProto v1 maximum encrypted frame payload.
	MaxReasonSize = MaxEncryptedPayloadSize - 2 - TLVHeaderSize - 1
)

var (
	// ErrMessageTooShort indicates that a message lacks its required fixed prefix.
	ErrMessageTooShort = errors.New("dgproto: message payload too short")
	// ErrMessageLength indicates that a message payload has an invalid encoded length.
	ErrMessageLength = errors.New("dgproto: invalid message payload length")
	// ErrMessageReserved indicates that a reserved message field is nonzero.
	ErrMessageReserved = errors.New("dgproto: reserved message field must be zero")
	// ErrInvalidNoisePattern indicates an unregistered Noise pattern value.
	ErrInvalidNoisePattern = errors.New("dgproto: invalid Noise pattern")
	// ErrHandshakeAlignment indicates that a handshake payload is not 4-byte aligned.
	ErrHandshakeAlignment = errors.New("dgproto: handshake payload must be 4-byte aligned")
	// ErrUnexpectedNoiseData indicates that a Noise XX initial wrapper carries extra payload.
	ErrUnexpectedNoiseData = errors.New("dgproto: Noise XX initial payload must be empty")
	// ErrInvalidPingResponse indicates that the ping response byte is neither zero nor one.
	ErrInvalidPingResponse = errors.New("dgproto: invalid ping response flag")
	// ErrAckCount indicates that an acknowledgement contains fewer than one or more than MaxAckSequences entries.
	ErrAckCount = errors.New("dgproto: acknowledgement count must be between 1 and 255")
	// ErrInvalidUTF8 indicates that a textual message field is not valid UTF-8.
	ErrInvalidUTF8 = errors.New("dgproto: text is not valid UTF-8")
	// ErrReasonTooLong indicates that a textual message field exceeds MaxReasonSize.
	ErrReasonTooLong = errors.New("dgproto: reason exceeds maximum length")
	// ErrUnknownMessageTLV indicates an unsupported TLV in a typed protocol message.
	ErrUnknownMessageTLV = errors.New("dgproto: unknown message TLV")
	// ErrDuplicateMessageTLV indicates repeated TLV types where a typed message requires uniqueness.
	ErrDuplicateMessageTLV = errors.New("dgproto: duplicate message TLV")
	// ErrInvalidCloseCode indicates a SessionClose code outside the MVP range zero through three.
	ErrInvalidCloseCode = errors.New("dgproto: invalid close code")
	// ErrResumptionTicket indicates an invalid post-MVP resumption-ticket encoding.
	ErrResumptionTicket = errors.New("dgproto: invalid resumption ticket")
)

// ResumptionTicket is a wire-registry representation reserved for post-MVP.
// The MVP Session API rejects this message type.
type ResumptionTicket struct {
	// Ticket contains the opaque post-MVP ticket bytes.
	Ticket []byte
	// ValidUntil contains the encoded ticket-expiration value.
	ValidUntil uint64
}

// MarshalBinary encodes the post-MVP ticket representation and returns owned storage.
func (m ResumptionTicket) MarshalBinary() ([]byte, error) {
	if len(m.Ticket) == 0 || len(m.Ticket) > MaxResumptionTicketSize {
		return nil, fmt.Errorf("%w: ticket length %d", ErrResumptionTicket, len(m.Ticket))
	}
	buf := make([]byte, len(m.Ticket)+8)
	wire.PutTicket(buf, m.Ticket, m.ValidUntil)
	return buf, nil
}

// UnmarshalBinary decodes the post-MVP ticket representation and copies Ticket.
func (m *ResumptionTicket) UnmarshalBinary(data []byte) error {
	if len(data) < 9 || len(data) > MaxEncryptedPayloadSize {
		return fmt.Errorf("%w: payload length %d", ErrResumptionTicket, len(data))
	}
	ticket, validUntil := wire.ReadTicket(data)
	*m = ResumptionTicket{Ticket: append([]byte(nil), ticket...), ValidUntil: validUntil}
	return nil
}

// PingPong carries a keepalive nonce and whether it is a response.
type PingPong struct {
	// IsResponse distinguishes a pong from a ping.
	IsResponse bool
	// Nonce correlates a response with its request.
	Nonce uint64
}

// MarshalBinary encodes m as the fixed-size ping or pong payload.
func (m PingPong) MarshalBinary() ([]byte, error) {
	buf := make([]byte, PingPongSize)
	wire.PutPingPong(buf, m.IsResponse, m.Nonce)
	return buf, nil
}

// UnmarshalBinary decodes a fixed-size ping or pong payload into m.
func (m *PingPong) UnmarshalBinary(data []byte) error {
	if len(data) != PingPongSize {
		return fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(data), PingPongSize)
	}
	if data[0] > 1 {
		return fmt.Errorf("%w: 0x%02x", ErrInvalidPingResponse, data[0])
	}
	response, nonce := wire.ReadPingPong(data)
	*m = PingPong{IsResponse: response, Nonce: nonce}
	return nil
}

// Ack selectively acknowledges one to 255 sequence numbers.
type Ack struct {
	// Sequences contains the acknowledged sequence numbers.
	Sequences []uint64
}

// MarshalBinary encodes one to MaxAckSequences sequence numbers.
func (m Ack) MarshalBinary() ([]byte, error) {
	if len(m.Sequences) < 1 || len(m.Sequences) > MaxAckSequences {
		return nil, fmt.Errorf("%w: got %d", ErrAckCount, len(m.Sequences))
	}
	buf := make([]byte, wire.AckSize(len(m.Sequences)))
	wire.PutAck(buf, m.Sequences)
	return buf, nil
}

// UnmarshalBinary decodes an acknowledgement payload into newly allocated Sequences.
func (m *Ack) UnmarshalBinary(data []byte) error {
	if len(data) < 1 {
		return ErrMessageTooShort
	}
	count := int(data[0])
	if count < 1 || count > MaxAckSequences {
		return fmt.Errorf("%w: got %d", ErrAckCount, count)
	}
	if len(data) != wire.AckSize(count) {
		return fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(data), wire.AckSize(count))
	}
	sequences := make([]uint64, count)
	wire.ReadAck(data, sequences)
	*m = Ack{Sequences: sequences}
	return nil
}

// RekeyInit announces a new epoch and carries its key confirmation value.
// Session generates outbound values internally; callers cannot send one through
// Send or SendPayload.
type RekeyInit struct {
	// Epoch is the nonzero proposed key epoch.
	Epoch uint32
	// KeyConfirm authenticates the proposed epoch key.
	KeyConfirm [32]byte
}

// MarshalBinary encodes m as the fixed-size rekey-init payload.
func (m RekeyInit) MarshalBinary() ([]byte, error) {
	if m.Epoch == 0 {
		return nil, ErrInvalidEpoch
	}
	buf := make([]byte, RekeyInitSize)
	wire.PutRekeyInit(buf, m.Epoch, m.KeyConfirm)
	return buf, nil
}

// UnmarshalBinary decodes a fixed-size rekey-init payload into m.
func (m *RekeyInit) UnmarshalBinary(data []byte) error {
	if len(data) != RekeyInitSize {
		return fmt.Errorf("%w: got %d, want %d", ErrMessageLength, len(data), RekeyInitSize)
	}
	epoch, confirm := wire.ReadRekeyInit(data)
	if epoch == 0 {
		return ErrInvalidEpoch
	}
	*m = RekeyInit{Epoch: epoch, KeyConfirm: confirm}
	return nil
}
