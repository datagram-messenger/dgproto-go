package dgproto

import (
	"errors"
	"testing"
)

func TestSessionSendRejectsNilMessagePointers(t *testing.T) {
	for _, test := range []struct {
		name    string
		message any
	}{
		{"data", (*EncryptedData)(nil)},
		{"ping", (*PingPong)(nil)},
		{"close", (*SessionClose)(nil)},
		{"ack", (*Ack)(nil)},
		{"error", (*ErrorMessage)(nil)},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("Send panicked on nil message: %v", recovered)
				}
			}()
			session, peer := testSessions(t)
			if _, err := session.Send(test.message, 0); !errors.Is(err, ErrMessageType) {
				t.Fatalf("nil message error = %v, want ErrMessageType", err)
			}
			frame, err := session.Send(PingPong{Nonce: 42}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Header.Sequence != 1 {
				t.Fatalf("sequence = %d, want 1", frame.Header.Sequence)
			}
			if _, err := peer.Receive(frame); err != nil {
				t.Fatal(err)
			}
		})
	}
}
