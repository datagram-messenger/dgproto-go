// Package echoapp contains example-local policy for the independent peers.
// It is not a supported client API or a key-management layer.
package echoapp

import (
	"bytes"
	"errors"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

const (
	Payload                 = "hello DGProto"
	streamID         uint16 = 1
	requestType      uint8  = 1
	replyType        uint8  = 2
	payloadField     uint8  = 1
	padLength        uint8  = 16
	operationTimeout        = time.Second
)

var ErrEnvelope = errors.New("echo: invalid application envelope")

// Validate checks the complete fixed demo schema, not just the payload text.
func Validate(data *dgproto.EncryptedData, operation uint8) error {
	if data == nil || data.StreamID != streamID || data.AppMessageType != operation ||
		len(data.Fields) != 1 || data.Fields[0].Type != payloadField || !bytes.Equal(data.Fields[0].Value, []byte(Payload)) {
		return ErrEnvelope
	}
	return nil
}
