// Package wire implements primitive DGProto header and control-payload byte codecs.
// It has no protocol-policy, error-sentinel or caller-buffer ownership state.
package wire

import "encoding/binary"

const (
	HeaderSize         = 40
	Version      uint8 = 1
	AEADTagSize        = 16
	MaxFrameSize       = 65535
)

// Header is a transient primitive wire representation, not the public API.
// Magic is an octet field supplied by the caller, not a cached protocol value.
// All flags and reserved bytes are lossless; semantic validation belongs to root.
type Header struct {
	Magic         [4]byte
	Version       uint8
	Flags         uint8
	MessageType   uint8
	Reserved      [4]byte
	SessionID     [16]byte
	Sequence      uint64
	PayloadLength uint32
	PadLength     uint8
}

// PutHeader writes exactly HeaderSize bytes, leaving any suffix untouched.
// dst must contain at least HeaderSize bytes. No validation or allocation occurs.
func PutHeader(dst []byte, h Header) {
	_ = dst[HeaderSize-1]
	copy(dst[0:4], h.Magic[:])
	dst[4] = h.Version
	dst[5] = h.Flags
	dst[6] = h.MessageType
	dst[7] = h.Reserved[0]
	copy(dst[8:24], h.SessionID[:])
	binary.LittleEndian.PutUint64(dst[24:32], h.Sequence)
	binary.LittleEndian.PutUint32(dst[32:36], h.PayloadLength)
	dst[36] = h.PadLength
	copy(dst[37:40], h.Reserved[1:])
}

// ReadHeader reads exactly HeaderSize bytes into a value with no borrowed storage.
// src must contain at least HeaderSize bytes. All octets are retained without
// validation; root checks length, current Magic and version before calling.
func ReadHeader(src []byte) Header {
	_ = src[HeaderSize-1]
	h := Header{
		Magic:   [4]byte(src[0:4]),
		Version: src[4], Flags: src[5], MessageType: src[6],
		Reserved:      [4]byte{src[7], src[37], src[38], src[39]},
		Sequence:      binary.LittleEndian.Uint64(src[24:32]),
		PayloadLength: binary.LittleEndian.Uint32(src[32:36]),
		PadLength:     src[36],
	}
	copy(h.SessionID[:], src[8:24])
	return h
}
