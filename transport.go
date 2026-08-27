package dgproto

import (
	"context"
	"errors"
)

var (
	// ErrTransportFrameTooShort indicates that a transport frame is shorter than HeaderSize.
	ErrTransportFrameTooShort = errors.New("dgproto: transport frame too short")
	// ErrTransportFrameTooLarge indicates that a transport frame exceeds MaxFrameSize.
	ErrTransportFrameTooLarge = errors.New("dgproto: transport frame too large")
	// ErrTransportClosed indicates that the underlying transport is closed.
	ErrTransportClosed = errors.New("dgproto: transport closed")
)

// FrameReader reads one complete DGProto v1 frame, blocking until the frame is
// available, the context is canceled, or an error occurs.
type FrameReader interface {
	ReadFrame(context.Context) (Frame, error)
}

// FrameWriter writes one complete DGProto v1 frame, blocking until all bytes are
// written, the context is canceled, or an error occurs.
type FrameWriter interface {
	WriteFrame(context.Context, Frame) error
}

// Transport reads, writes, and closes a framed connection.
type Transport interface {
	FrameReader
	FrameWriter
	Close() error
}
