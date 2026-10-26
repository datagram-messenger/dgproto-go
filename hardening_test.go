package dgproto

import (
	"errors"
	"testing"
)

func TestHardeningPendingRekeyOwned(t *testing.T) {
	for _, part := range []string{"payload", "padding"} {
		t.Run(part, func(t *testing.T) {
			s, _ := testSessions(t)
			s.rekeyFrameLimit = 1
			if _, err := s.Send(PingPong{Nonce: 1}, 0); err != nil {
				t.Fatal(err)
			}
			frame, err := s.Send(PingPong{Nonce: 2}, 8)
			if err != nil {
				t.Fatal(err)
			}
			original := frame
			original.Payload = append([]byte(nil), frame.Payload...)
			original.Padding = append([]byte(nil), frame.Padding...)
			if part == "payload" {
				frame.Payload[0] ^= 1
			} else {
				frame.Padding[0] ^= 1
			}
			if err := s.MarkRekeySent(frame); !errors.Is(err, ErrRekeyPending) {
				t.Fatalf("mutated frame accepted: %v", err)
			}
			if err := s.MarkRekeySent(original); err != nil {
				t.Fatalf("original rejected: %v", err)
			}
		})
	}
}
