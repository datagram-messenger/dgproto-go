package dgproto

import (
	"encoding"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrSessionClosed indicates an operation on a closed or nil session.
	ErrSessionClosed = errors.New("dgproto: session is closed")
	// ErrWrongSession indicates that a frame carries a different session ID.
	ErrWrongSession = errors.New("dgproto: frame belongs to another session")
	// ErrSequenceExhausted indicates that no further send sequence can be allocated.
	ErrSequenceExhausted = errors.New("dgproto: send sequence exhausted")
	// ErrMessageType indicates that a value or frame type is unavailable through the strict-MVP Session API.
	ErrMessageType = errors.New("dgproto: message does not match encrypted frame type")
)

// HandshakeSecrets is the directional material needed to open a session.
type HandshakeSecrets struct {
	SessionID  [16]byte
	SendKey    [KeySize]byte
	ReceiveKey [KeySize]byte
}

// Secrets returns an owned copy of the session material established by a handshake.
func (r HandshakeResult) Secrets() HandshakeSecrets {
	return HandshakeSecrets{SessionID: r.SessionID, SendKey: r.SendKey, ReceiveKey: r.ReceiveKey}
}

// Session owns directional codecs, sequence allocation, and receive replay
// state. Its exported methods are safe for concurrent use. The Session API is
// strict MVP: it rejects post-MVP message type 0x07.
type Session struct {
	sessionID [16]byte
	suite     CipherSuite

	sendMu          sync.Mutex
	send            *Codec
	sendKey         [KeySize]byte
	sendEpoch       uint32
	nextSequence    uint64
	sentInEpoch     uint64
	epochStarted    time.Time
	rekeyFrameLimit uint64
	rekeyInterval   time.Duration
	now             func() time.Time
	pendingRekey    *pendingSendRekey
	closed          bool

	receiveMu       sync.Mutex
	receive         *Codec
	receiveKey      [KeySize]byte
	receiveEpoch    uint32
	replay          ReplayWindow
	previousReceive *Codec
	previousReplay  ReplayWindow
	graceRemaining  uint64
	graceUntil      time.Time
	graceFrames     uint64
	gracePeriod     time.Duration
}

// NewSession opens a session from role-oriented handshake secrets.
func NewSession(suite CipherSuite, secrets HandshakeSecrets) (*Session, error) {
	if suite != CipherChaCha20Poly1305 {
		return nil, fmt.Errorf("%w: DGProto v1 requires ChaCha20-Poly1305", ErrUnsupportedCipher)
	}
	if secrets.SessionID == ([16]byte{}) {
		return nil, ErrInvalidSessionID
	}
	send, err := NewCodec(suite, secrets.SendKey[:])
	if err != nil {
		return nil, err
	}
	receive, err := NewCodec(suite, secrets.ReceiveKey[:])
	if err != nil {
		return nil, err
	}
	now := time.Now
	keylogWrite(secrets.SessionID, secrets.SendKey, secrets.ReceiveKey)
	return &Session{
		sessionID:       secrets.SessionID,
		suite:           suite,
		send:            send,
		sendKey:         secrets.SendKey,
		sendEpoch:       1,
		nextSequence:    1,
		epochStarted:    now(),
		rekeyFrameLimit: DefaultRekeyFrameLimit,
		rekeyInterval:   DefaultRekeyInterval,
		now:             now,
		receive:         receive,
		receiveKey:      secrets.ReceiveKey,
		receiveEpoch:    1,
		graceFrames:     DefaultRekeyGraceFrames,
		gracePeriod:     DefaultRekeyGracePeriod,
	}, nil
}

// NewSessionFromHandshake opens a session from a completed handshake result.
func NewSessionFromHandshake(suite CipherSuite, result HandshakeResult) (*Session, error) {
	return NewSession(suite, result.Secrets())
}

// SessionID returns this session's identifier.
func (s *Session) SessionID() [16]byte {
	if s == nil {
		return [16]byte{}
	}
	return s.sessionID
}

// Close prevents subsequent sends and receives. It is idempotent.
func (s *Session) Close() error {
	if s == nil {
		return ErrSessionClosed
	}
	s.sendMu.Lock()
	s.receiveMu.Lock()
	s.closed = true
	s.receiveMu.Unlock()
	s.sendMu.Unlock()
	return nil
}

// Closed reports whether the session has been closed.
func (s *Session) Closed() bool {
	if s == nil {
		return true
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.closed
}

// ErrRekeyPending indicates that Send is blocked until the caller confirms
// that the previously returned RekeyInit was transmitted.
var ErrRekeyPending = errors.New("dgproto: rekey frame has not been marked sent")

type pendingSendRekey struct {
	frame Frame
	key   [KeySize]byte
	codec *Codec
	epoch uint32
}

func validSessionMessageType(messageType MessageType) bool {
	return messageType >= MessageTypeEncryptedData && messageType <= MessageTypeError
}

func outboundMessage(message any) (MessageType, encoding.BinaryMarshaler, error) {
	var messageType MessageType
	switch m := message.(type) {
	case EncryptedData:
		messageType = MessageTypeEncryptedData
	case *EncryptedData:
		if m == nil {
			return 0, nil, ErrMessageType
		}
		messageType = MessageTypeEncryptedData
	case PingPong:
		messageType = MessageTypePingPong
	case *PingPong:
		if m == nil {
			return 0, nil, ErrMessageType
		}
		messageType = MessageTypePingPong
	case SessionClose:
		messageType = MessageTypeSessionClose
	case *SessionClose:
		if m == nil {
			return 0, nil, ErrMessageType
		}
		messageType = MessageTypeSessionClose
	case Ack:
		messageType = MessageTypeAck
	case *Ack:
		if m == nil {
			return 0, nil, ErrMessageType
		}
		messageType = MessageTypeAck
	case ErrorMessage:
		messageType = MessageTypeError
	case *ErrorMessage:
		if m == nil {
			return 0, nil, ErrMessageType
		}
		messageType = MessageTypeError
	default:
		return 0, nil, fmt.Errorf("%w: %T", ErrMessageType, message)
	}
	marshaler, ok := message.(encoding.BinaryMarshaler)
	if !ok || marshaler == nil {
		return 0, nil, fmt.Errorf("%w: %T", ErrMessageType, message)
	}
	return messageType, marshaler, nil
}

func newInboundMessage(messageType MessageType) (encoding.BinaryUnmarshaler, error) {
	switch messageType {
	case MessageTypeEncryptedData:
		return &EncryptedData{}, nil
	case MessageTypePingPong:
		return &PingPong{}, nil
	case MessageTypeSessionClose:
		return &SessionClose{}, nil
	case MessageTypeAck:
		return &Ack{}, nil
	case MessageTypeRekeyInit:
		return &RekeyInit{}, nil
	case MessageTypeError:
		return &ErrorMessage{}, nil
	default:
		return nil, fmt.Errorf("%w: 0x%02x", ErrMessageType, messageType)
	}
}
