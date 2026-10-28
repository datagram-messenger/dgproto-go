package dgproto_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	dg "github.com/datagram-messenger/dgproto-go"
)

// Cover every uint8 padding budget with multiple independently aligned fields.
// The original release cases cover representative padding and single-field edges;
// this pins aggregate alignment at the last fitting and first oversized payload.
func TestEncryptedDataSizeAllPaddingAggregateBoundaries(t *testing.T) {
	codec, err := dg.NewCodec(dg.CipherChaCha20Poly1305, make([]byte, dg.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	var sequence uint64
	for padding := 0; padding <= 255; padding++ {
		t.Run(fmt.Sprintf("pad%d", padding), func(t *testing.T) {
			pad := uint8(padding)
			budget := (dg.MaxFrameSize - dg.HeaderSize - dg.AEADTagSize - padding) &^ 3
			for _, extra := range []int{0, 4} {
				// Envelope 4 + empty TLV 4 + five-byte TLV 8 + final TLV.
				m := dg.EncryptedData{StreamID: 65535, AppMessageType: 255, Fields: []dg.TLV{
					{Type: 255},
					{Type: 0, Value: []byte("hello")},
					{Type: 128, Value: bytes.Repeat([]byte{0xa5}, budget-16-dg.TLVHeaderSize+extra)},
				}}
				n, sizeErr := m.EncodedLen()
				wire, marshalErr := m.MarshalBinary()
				preflightErr := m.ValidateFrameSize(pad)
				if budget+extra > dg.MaxEncryptedPayloadSize {
					if n != 0 || wire != nil || !errors.Is(sizeErr, dg.ErrMessageLength) || !errors.Is(marshalErr, dg.ErrMessageLength) || !errors.Is(preflightErr, dg.ErrMessageLength) {
						t.Fatalf("envelope must precede frame error: len=%d size=%v marshal=%v preflight=%v", n, sizeErr, marshalErr, preflightErr)
					}
					continue
				}
				if sizeErr != nil || marshalErr != nil || n != budget+extra || len(wire) != n {
					t.Fatalf("aggregate size: len=%d wire=%d size=%v marshal=%v", n, len(wire), sizeErr, marshalErr)
				}
				sequence++ // Each encryption attempt uses a new synthetic nonce.
				frame, encryptErr := codec.Encrypt(dg.MessageTypeEncryptedData, [16]byte{1}, sequence, wire, pad)
				if extra != 0 {
					if !errors.Is(preflightErr, dg.ErrPayloadTooLarge) || !errors.Is(encryptErr, dg.ErrPayloadTooLarge) || preflightErr.Error() != encryptErr.Error() {
						t.Fatalf("oversized aggregate: preflight=%v encrypt=%v", preflightErr, encryptErr)
					}
					continue
				}
				if preflightErr != nil || encryptErr != nil {
					t.Fatalf("last fitting aggregate: preflight=%v encrypt=%v", preflightErr, encryptErr)
				}
				encodedFrame, err := frame.MarshalBinary()
				if err != nil || len(encodedFrame) != dg.HeaderSize+n+dg.AEADTagSize+padding || dg.MaxFrameSize-len(encodedFrame) > 3 {
					t.Fatalf("incorrect final frame budget: bytes=%d err=%v", len(encodedFrame), err)
				}
				plain, err := codec.Decrypt(frame)
				if err != nil || !bytes.Equal(plain, wire) {
					t.Fatalf("aggregate round trip: %v", err)
				}
			}
		})
	}
}
