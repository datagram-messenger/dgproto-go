package dgproto_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestPinnedExampleCloseError(t *testing.T) {
	for _, err := range []error{nil, io.EOF, dgproto.ErrConnectionClosed} {
		if got := pinnedCloseError(err); got != nil {
			t.Fatalf("normal close %v reported as %v", err, got)
		}
	}
	unexpected := errors.New("close failed")
	for _, err := range []error{
		unexpected,
		context.Canceled,
		context.DeadlineExceeded,
		io.ErrUnexpectedEOF,
		fmt.Errorf("write: %w", io.EOF),
		errors.Join(io.EOF, unexpected),
		errors.Join(dgproto.ErrConnectionClosed, unexpected),
	} {
		if got := pinnedCloseError(err); got != err {
			t.Fatalf("unexpected close %v lost: %v", err, got)
		}
	}
}

func TestPinnedExampleExchange(t *testing.T) {
	for _, start := range []bool{false, true} {
		name := "DialContext"
		if start {
			name = "DialAndStartContext"
		}
		t.Run(name, func(t *testing.T) {
			if err := pinnedExchange(start); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPinnedExampleEnvelope(t *testing.T) {
	for _, data := range []*dgproto.EncryptedData{
		nil,
		{StreamID: 2, AppMessageType: 2},
		{StreamID: 1, AppMessageType: 1},
		{StreamID: 1, AppMessageType: 2, Fields: []dgproto.TLV{{Type: 1}}},
	} {
		if validatePinnedEnvelope(data, 2) == nil {
			t.Fatal("invalid reply accepted")
		}
	}
	if err := validatePinnedEnvelope(&dgproto.EncryptedData{StreamID: 1, AppMessageType: 2}, 2); err != nil {
		t.Fatal(err)
	}
}
