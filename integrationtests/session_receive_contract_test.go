package integrationtests

import (
	"errors"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Only public APIs and synthetic traffic material are used. Explicit sequences
// keep replay boundaries deterministic without a time-triggered sender rekey.
func TestSessionAuthenticationBeforeReplay(t *testing.T) {
	for _, previous := range []bool{false, true} {
		name := "current"
		if previous {
			name = "previous during grace"
		}
		t.Run(name, func(t *testing.T) {
			_, receiver := runtimeSessions(t)
			key := [32]byte{2} // runtimeSessions' initiator-to-responder key.
			codec, err := dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, key[:])
			if err != nil {
				t.Fatal(err)
			}
			payload, err := (dgproto.PingPong{Nonce: 17}).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			frame := func(c *dgproto.Codec, sequence uint64) dgproto.Frame {
				t.Helper()
				f, err := c.Encrypt(dgproto.MessageTypePingPong, receiver.SessionID(), sequence, payload, 0)
				if err != nil {
					t.Fatal(err)
				}
				return f
			}
			accept := func(f dgproto.Frame) {
				t.Helper()
				if _, err := receiver.Receive(f); err != nil {
					t.Fatalf("authentic fresh frame: %v", err)
				}
			}
			old := frame(codec, 1)
			duplicate := frame(codec, dgproto.ReplayWindowSize+4)
			accept(old)
			accept(duplicate)
			currentCodec := codec
			if previous {
				confirm, err := dgproto.NewRekeyState().ComputeKeyConfirm(key[:], 2)
				if err != nil {
					t.Fatal(err)
				}
				init, err := (dgproto.RekeyInit{Epoch: 2, KeyConfirm: confirm}).MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				rekey, err := codec.Encrypt(dgproto.MessageTypeRekeyInit, receiver.SessionID(), dgproto.ReplayWindowSize+5, init, 0)
				if err != nil {
					t.Fatal(err)
				}
				accept(rekey)
				next, _, err := dgproto.DeriveNextKeys(key[:])
				if err != nil {
					t.Fatal(err)
				}
				currentCodec, err = dgproto.NewCodec(dgproto.CipherChaCha20Poly1305, next)
				if err != nil {
					t.Fatal(err)
				}
				// Current sequence 1 is a duplicate, but previous sequence 1 is
				// too old. Classification must use the authenticated epoch.
				accept(frame(currentCodec, 1))
			}
			for _, tc := range []struct {
				name string
				f    dgproto.Frame
				want error
			}{
				{"duplicate", duplicate, dgproto.ErrReplayDuplicate},
				{"too old", old, dgproto.ErrReplayTooOld},
			} {
				t.Run(tc.name, func(t *testing.T) {
					forged := tc.f
					forged.Tag[0] ^= 1
					if _, err := receiver.Receive(forged); !errors.Is(err, dgproto.ErrAuthentication) {
						t.Errorf("forged frame = %v, want ErrAuthentication", err)
					}
					if _, err := receiver.Receive(tc.f); !errors.Is(err, tc.want) {
						t.Errorf("authentic replay = %v, want %v", err, tc.want)
					}
				})
			}
			// A failed fresh authentication must not consume its sequence.
			fresh := frame(currentCodec, 2*dgproto.ReplayWindowSize+10)
			forged := fresh
			forged.Tag[0] ^= 1
			if _, err := receiver.Receive(forged); !errors.Is(err, dgproto.ErrAuthentication) {
				t.Fatalf("forged fresh frame = %v", err)
			}
			accept(fresh)
			if previous {
				// Still in grace; the current high-water mark cannot veto a
				// fresh previous-key sequence that is too old in current state.
				accept(frame(codec, dgproto.ReplayWindowSize+3))
			}
		})
	}
}
