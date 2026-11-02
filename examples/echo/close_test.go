package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestClosePreservesCompositeCauses(t *testing.T) {
	unexpected := errors.New("unexpected close failure")
	for _, benign := range []error{io.EOF, net.ErrClosed, dgproto.ErrConnectionClosed} {
		if err := joinClientClose(nil, benign); err != nil {
			t.Fatal(err)
		}
		for _, closeErr := range []error{fmt.Errorf("wrapped: %w", benign), errors.Join(benign, unexpected)} {
			got := joinClientClose(nil, closeErr)
			if !errors.Is(got, closeErr) {
				t.Fatalf("close cause discarded: %v", closeErr)
			}
		}
		if got := joinClientClose(unexpected, benign); !errors.Is(got, unexpected) || !errors.Is(got, benign) {
			t.Fatal("operation/close causes lost")
		}
	}
}
