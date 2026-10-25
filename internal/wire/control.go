package wire

import "encoding/binary"

// Fixed control-payload shapes. Message policy and public error variables stay
// in root; readers and writers below require fully validated buffers.
const (
	PingPongSize    = 9
	RekeyInitSize   = 36
	MaxAckSequences = 255
)

// PutPingPong writes the entire fixed payload. dst must have PingPongSize bytes.
func PutPingPong(dst []byte, response bool, nonce uint64) {
	_ = dst[PingPongSize-1]
	dst[0] = 0
	if response {
		dst[0] = 1
	}
	binary.LittleEndian.PutUint64(dst[1:], nonce)
}

// ReadPingPong requires a validated PingPongSize-byte payload and response flag.
func ReadPingPong(src []byte) (bool, uint64) {
	_ = src[PingPongSize-1]
	return src[0] == 1, binary.LittleEndian.Uint64(src[1:])
}

// AckSize returns the encoded size for a validated count in [1, MaxAckSequences].
func AckSize(count int) int { return 1 + 8*count }

// PutAck writes the count and every sequence, leaving a suffix untouched.
// The count must be validated and dst must contain AckSize(len(sequences)) bytes.
func PutAck(dst []byte, sequences []uint64) {
	_ = dst[AckSize(len(sequences))-1]
	dst[0] = byte(len(sequences))
	for i, sequence := range sequences {
		binary.LittleEndian.PutUint64(dst[1+8*i:], sequence)
	}
}

// ReadAck fills caller-owned storage. src must be fully validated and sequences
// must have exactly the encoded count. Neither input nor output is retained.
func ReadAck(src []byte, sequences []uint64) {
	_ = src[AckSize(len(sequences))-1]
	for i := range sequences {
		sequences[i] = binary.LittleEndian.Uint64(src[1+8*i:])
	}
}

// PutRekeyInit writes the fixed epoch/confirmation payload; dst needs 36 bytes.
func PutRekeyInit(dst []byte, epoch uint32, confirm [32]byte) {
	_ = dst[RekeyInitSize-1]
	binary.LittleEndian.PutUint32(dst[:4], epoch)
	copy(dst[4:RekeyInitSize], confirm[:])
}

// ReadRekeyInit reads a length-validated payload without borrowing storage.
// Epoch validation still belongs to the public wrapper and Session transaction.
func ReadRekeyInit(src []byte) (uint32, [32]byte) {
	_ = src[RekeyInitSize-1]
	var confirm [32]byte
	copy(confirm[:], src[4:RekeyInitSize])
	return binary.LittleEndian.Uint32(src[:4]), confirm
}

// PutTicket writes the post-MVP registry representation, not Session support.
// The ticket length must be validated and dst needs len(ticket)+8 bytes.
func PutTicket(dst, ticket []byte, validUntil uint64) {
	_ = dst[len(ticket)+8-1]
	copy(dst[:len(ticket)], ticket)
	binary.LittleEndian.PutUint64(dst[len(ticket):], validUntil)
}

// ReadTicket returns a borrowed ticket view and expiration from a validated
// payload of at least nine bytes. The public wrapper must copy the view before
// assigning its destination. No input is retained and no allocation occurs.
func ReadTicket(src []byte) ([]byte, uint64) {
	_ = src[8]
	end := len(src) - 8
	return src[:end], binary.LittleEndian.Uint64(src[end:])
}
