package dgproto_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	. "github.com/datagram-messenger/dgproto-go"
	"testing"
)

func TestRekeyStateEpochBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name          string
		current, next uint32
		want          error
	}{
		{"zero state", 0, 1, ErrInvalidEpoch},
		{"zero proposal", 1, 0, ErrInvalidEpoch},
		{"next", 1, 2, nil},
		{"last next", ^uint32(0) - 1, ^uint32(0), nil},
		{"exhausted", ^uint32(0), 0, ErrEpochExhausted},
		{"skipped", 1, 3, ErrInvalidEpoch},
		{"duplicate", 2, 2, ErrInvalidEpoch},
		{"rollback", 2, 1, ErrInvalidEpoch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := RekeyState{Epoch: tc.current}
			secret := make([]byte, KeySize)
			got, err := r.ComputeKeyConfirm(secret, tc.next)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if r.Epoch != tc.current {
				t.Fatal("state mutated")
			}
			if err != nil {
				if got != [32]byte{} {
					t.Fatal("confirmation returned on error")
				}
				return
			}
			mac := hmac.New(sha256.New, secret)
			mac.Write([]byte("DGPv1 Rekey Confirm"))
			var epoch [4]byte
			binary.LittleEndian.PutUint32(epoch[:], tc.next)
			mac.Write(epoch[:])
			if !hmac.Equal(got[:], mac.Sum(nil)) {
				t.Fatal("confirmation bytes changed")
			}
		})
	}
	var r *RekeyState
	if _, err := r.ComputeKeyConfirm(nil, 1); !errors.Is(err, ErrEpochExhausted) {
		t.Fatalf("nil error=%v", err)
	}
}
