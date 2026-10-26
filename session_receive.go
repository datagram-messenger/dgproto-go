package dgproto

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"math"
	"time"
)

// Receive authenticates a frame, commits its sequence, and decodes its message.
// Reserved inbound header flags are ignored. Authentication failures do not
// commit replay state. RekeyInit is fully decoded and validated before its key,
// epoch, replay, and grace state are committed atomically. For other message
// types, an authenticated but malformed payload still consumes its sequence;
// retrying the same frame is therefore rejected as a replay.
func (s *Session) Receive(frame Frame) (any, error) {
	plaintext, err := s.ReceivePayload(frame)
	if err != nil {
		return nil, err
	}
	message, err := newInboundMessage(frame.Header.MessageType)
	if err != nil {
		return nil, err
	}
	if err := message.UnmarshalBinary(plaintext); err != nil {
		return nil, err
	}
	return message, nil
}

// ReceivePayload validates and authenticates a frame before committing any
// receive state. Rekey validation, replay advancement, and the key transition
// are committed together while receiveMu is held.
func (s *Session) ReceivePayload(frame Frame) ([]byte, error) {
	if frame.Header.MessageType == MessageTypeResumptionTicket {
		return nil, fmt.Errorf("%w: 0x%02x is post-MVP", ErrMessageType, frame.Header.MessageType)
	}
	if s == nil {
		return nil, ErrSessionClosed
	}
	if frame.Header.SessionID != s.sessionID {
		return nil, ErrWrongSession
	}
	if !validSessionMessageType(frame.Header.MessageType) {
		return nil, fmt.Errorf("%w: 0x%02x", ErrMessageType, frame.Header.MessageType)
	}

	s.receiveMu.Lock()
	defer s.receiveMu.Unlock()
	if s.closed {
		return nil, ErrSessionClosed
	}

	candidate, err := s.decryptEpochLocked(frame)
	if err != nil {
		return nil, err
	}
	if !candidate.current && frame.Header.MessageType == MessageTypeRekeyInit {
		return nil, fmt.Errorf("%w: rekey from previous epoch", ErrInvalidEpoch)
	}

	if candidate.current && frame.Header.MessageType == MessageTypeRekeyInit {
		var init RekeyInit
		if err := init.UnmarshalBinary(candidate.plaintext); err != nil {
			return nil, err
		}
		nextKey, nextCodec, err := s.prepareRekeyLocked(init)
		if err != nil {
			return nil, err
		}

		committedReplay := s.replay
		if err := committedReplay.Commit(candidate.token); err != nil {
			return nil, err
		}
		s.previousReceive, s.previousReplay = s.receive, committedReplay
		s.receive, s.receiveKey, s.receiveEpoch = nextCodec, nextKey, init.Epoch
		s.replay = ReplayWindow{}
		s.graceRemaining = s.graceFrames
		s.graceUntil = s.now().Add(s.gracePeriod)
		s.expireGraceLocked()
		keylogWriteEpoch(s.sessionID, "recv", s.receiveEpoch, s.receiveKey)
		return candidate.plaintext, nil
	}

	if candidate.current {
		committedReplay := s.replay
		if err := committedReplay.Commit(candidate.token); err != nil {
			return nil, err
		}
		s.replay = committedReplay
		if s.previousReceive != nil {
			if s.graceRemaining > 0 {
				s.graceRemaining--
			}
			s.expireGraceLocked()
		}
	} else {
		committedReplay := s.previousReplay
		if err := committedReplay.Commit(candidate.token); err != nil {
			return nil, err
		}
		s.previousReplay = committedReplay
	}
	return candidate.plaintext, nil
}

type receiveCandidate struct {
	plaintext []byte
	current   bool
	token     ReplayToken
}

func (s *Session) decryptEpochLocked(frame Frame) (receiveCandidate, error) {
	currentToken, currentCheck := s.replay.Check(frame.Header.Sequence)
	if currentCheck == nil {
		plaintext, err := s.receive.Decrypt(frame)
		if err == nil {
			return receiveCandidate{plaintext: plaintext, current: true, token: currentToken}, nil
		}
		if !errors.Is(err, ErrAuthentication) {
			return receiveCandidate{}, err
		}
	}

	previousAvailable := s.previousReceive != nil && s.graceRemaining > 0 &&
		s.gracePeriod > 0 && s.now().Before(s.graceUntil)
	if previousAvailable {
		previousToken, previousCheck := s.previousReplay.Check(frame.Header.Sequence)
		if previousCheck == nil || frame.Header.MessageType == MessageTypeRekeyInit {
			plaintext, err := s.previousReceive.Decrypt(frame)
			if err == nil {
				return receiveCandidate{plaintext: plaintext, current: false, token: previousToken}, nil
			}
			if !errors.Is(err, ErrAuthentication) {
				return receiveCandidate{}, err
			}
		}
		if currentCheck != nil && previousCheck != nil {
			return receiveCandidate{}, currentCheck
		}
	}
	if currentCheck != nil {
		return receiveCandidate{}, currentCheck
	}
	return receiveCandidate{}, ErrAuthentication
}

func (s *Session) prepareRekeyLocked(init RekeyInit) ([KeySize]byte, *Codec, error) {
	if s.receiveEpoch == math.MaxUint32 {
		return [KeySize]byte{}, nil, ErrEpochExhausted
	}
	if init.Epoch != s.receiveEpoch+1 {
		return [KeySize]byte{}, nil, fmt.Errorf("%w: got %d, want %d", ErrInvalidEpoch, init.Epoch, s.receiveEpoch+1)
	}
	expected, err := (&RekeyState{Epoch: s.receiveEpoch}).ComputeKeyConfirm(s.receiveKey[:], init.Epoch)
	if err != nil {
		return [KeySize]byte{}, nil, err
	}
	if !hmac.Equal(expected[:], init.KeyConfirm[:]) {
		return [KeySize]byte{}, nil, ErrKeyConfirmFailed
	}
	nextKey := deriveNextTrafficKey(s.receiveKey)
	nextCodec, err := NewCodec(s.suite, nextKey[:])
	if err != nil {
		return [KeySize]byte{}, nil, err
	}
	return nextKey, nextCodec, nil
}

func (s *Session) expireGraceLocked() {
	// The retained key is usable only while both grace budgets remain; clearing
	// it eagerly prevents a later frame from reviving an expired epoch.
	if s.previousReceive == nil {
		return
	}
	if s.graceRemaining == 0 || s.gracePeriod <= 0 || !s.now().Before(s.graceUntil) {
		s.previousReceive = nil
		s.previousReplay = ReplayWindow{}
		s.graceRemaining = 0
		s.graceUntil = time.Time{}
	}
}
