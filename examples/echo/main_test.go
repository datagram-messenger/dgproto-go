package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestJoinClientClose(t *testing.T) {
	for _, closeErr := range []error{io.EOF, net.ErrClosed, dgproto.ErrConnectionClosed} {
		if err := joinClientClose(nil, closeErr); err != nil {
			t.Fatalf("validated receipt undone: %v", err)
		}
		if err := joinClientClose(context.Canceled, closeErr); !errors.Is(err, context.Canceled) || !errors.Is(err, closeErr) {
			t.Fatal("operation or cleanup failure lost")
		}
	}
	if err := joinClientClose(nil, context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unexpected cleanup failure hidden")
	}
}

func TestWaitEchoPublishedResultWinsDone(t *testing.T) {
	for _, result := range []error{nil, errors.New("invalid reply")} {
		for range 100 {
			received := make(chan error, 1)
			received <- result
			done := make(chan struct{})
			close(done)
			if err := waitEcho(context.Background(), received, done, func() error { return dgproto.ErrConnectionClosed }); err != result {
				t.Fatalf("published result lost: %v", err)
			}
		}
	}
}

func TestWaitEchoDoneWithoutReplyFails(t *testing.T) {
	done := make(chan struct{})
	close(done)
	for _, cause := range []error{nil, dgproto.ErrConnectionClosed, io.EOF} {
		err := waitEcho(context.Background(), make(chan error), done, func() error { return cause })
		if err == nil || (cause != nil && !errors.Is(err, cause)) {
			t.Fatalf("termination became success or lost cause: %v", err)
		}
	}
}

func TestRunRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := run(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != successOutput {
		t.Fatalf("output = %q, want %q", got, successOutput)
	}
}

func TestRunCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := run(ctx, &out); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if out.Len() != 0 {
		t.Fatalf("canceled run produced output %q", out.String())
	}
}

func TestRunExpired(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var out bytes.Buffer
	if err := run(ctx, &out); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expired run produced output %q", out.String())
	}
}

func TestValidateEcho(t *testing.T) {
	valid := func() *dgproto.EncryptedData {
		return &dgproto.EncryptedData{StreamID: 1, AppMessageType: 2,
			Fields: []dgproto.TLV{{Type: 1, Value: []byte(demoPayload)}}}
	}
	if err := validateEcho(valid(), 2); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*dgproto.EncryptedData)
	}{
		{"stream", func(d *dgproto.EncryptedData) { d.StreamID = 2 }},
		{"operation", func(d *dgproto.EncryptedData) { d.AppMessageType = 1 }},
		{"missing payload", func(d *dgproto.EncryptedData) { d.Fields = nil }},
		{"extra field", func(d *dgproto.EncryptedData) { d.Fields = append(d.Fields, dgproto.TLV{Type: 2}) }},
		{"field type", func(d *dgproto.EncryptedData) { d.Fields[0].Type = 2 }},
		{"payload", func(d *dgproto.EncryptedData) { d.Fields[0].Value = []byte("wrong") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := valid()
			tt.change(data)
			if err := validateEcho(data, 2); err == nil {
				t.Fatal("invalid reply accepted")
			}
		})
	}
	if err := validateEcho(nil, 2); err == nil {
		t.Fatal("nil reply accepted")
	}
}
