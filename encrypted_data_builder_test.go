package dgproto_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func TestNewEncryptedDataOwnedSnapshot(t *testing.T) {
	value := []byte("hello")
	fields := []dgproto.TLV{{Type: 1, Value: value}, {Type: 255, Value: value}}
	m, err := dgproto.NewEncryptedData(65535, 255, fields...)
	if err != nil {
		t.Fatal(err)
	}
	want, err := m.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	value[0] = 'X'
	fields[0].Type = 2
	fields[1].Value = nil
	got, err := m.MarshalBinary()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("constructor retained caller storage")
	}
	m.Fields[0].Value[0] = 'Y'
	if string(m.Fields[1].Value) != "hello" || value[0] != 'X' {
		t.Fatal("field values share storage")
	}
	copy, err := dgproto.NewEncryptedData(m.StreamID, m.AppMessageType, m.Fields...)
	if err != nil {
		t.Fatal(err)
	}
	copy.Fields[0].Value[0] = 'Z'
	if m.Fields[0].Value[0] != 'Y' {
		t.Fatal("snapshot shares storage with source")
	}
}

func TestNewEncryptedDataValidationParity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields []dgproto.TLV
		want   error
	}{
		{"empty", nil, nil},
		{"empty nonnil", []dgproto.TLV{}, nil},
		{"empty value", []dgproto.TLV{{Type: 0}}, nil},
		{"unknown types", []dgproto.TLV{{Type: 0, Value: []byte{1}}, {Type: 255}}, nil},
		{"duplicate before length", []dgproto.TLV{{Type: 1, Value: make([]byte, dgproto.MaxTLVValueSize+1)}, {Type: 1}}, dgproto.ErrDuplicateMessageTLV},
		{"value overflow", []dgproto.TLV{{Type: 1, Value: make([]byte, dgproto.MaxTLVValueSize+1)}}, dgproto.ErrTLVValueTooLarge},
		{"sequence overflow", []dgproto.TLV{{Type: 1, Value: make([]byte, dgproto.MaxTLVValueSize)}}, dgproto.ErrTLVSequenceLimit},
		{"envelope overflow", []dgproto.TLV{{Type: 1, Value: make([]byte, dgproto.MaxEncryptedPayloadSize-4)}}, dgproto.ErrMessageLength},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := dgproto.EncryptedData{StreamID: 7, AppMessageType: 42, Fields: tc.fields}
			_, reference := source.EncodedLen()
			m, err := dgproto.NewEncryptedData(7, 42, tc.fields...)
			if (reference == nil) != (err == nil) || !errors.Is(err, tc.want) {
				t.Fatalf("validation parity: reference=%v got=%v", reference, err)
			}
			if err != nil {
				if !reflect.DeepEqual(m, dgproto.EncryptedData{}) {
					t.Fatal("invalid input returned partial envelope")
				}
				return
			}
			want, _ := source.MarshalBinary()
			got, e := m.MarshalBinary()
			if e != nil || !bytes.Equal(want, got) {
				t.Fatal("wire representation changed")
			}
			if (tc.fields == nil) != (m.Fields == nil) {
				t.Fatal("nil field slice not preserved")
			}
		})
	}
}

func TestNewEncryptedDataExactFrameBudget(t *testing.T) {
	// Largest aligned field sequence that fits the encrypted payload.
	m, err := dgproto.NewEncryptedData(1, 1, dgproto.TLV{Type: 1, Value: make([]byte, 65469)})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateFrameSize(3); err != nil {
		t.Fatal(err)
	}
	if err := m.ValidateFrameSize(4); !errors.Is(err, dgproto.ErrPayloadTooLarge) {
		t.Fatalf("padding budget: %v", err)
	}
}

func TestNewEncryptedDataQueuedInputReuse(t *testing.T) {
	received := make(chan string, 1)
	address, config := startedClientFixture(t, func(ctx context.Context, _ *dgproto.Connection, message any) error {
		if m, ok := message.(*dgproto.EncryptedData); ok && len(m.Fields) == 1 {
			select {
			case received <- string(m.Fields[0].Value):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := dgproto.DialContext(ctx, address, config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); awaitStartedClient(t, c) }()
	input := []byte("original")
	fields := []dgproto.TLV{{Type: 1, Value: input}}
	message, err := dgproto.NewEncryptedData(1, 2, fields...)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Send(message); err != nil {
		t.Fatal(err)
	}
	// Queue is deliberately stopped. Reuse only constructor inputs, not message.
	copy(input, "modified")
	fields[0].Type = 3
	c.Start(ctx)
	select {
	case got := <-received:
		if got != "original" {
			t.Fatal("queued envelope used reused input")
		}
	case <-ctx.Done():
		t.Fatal("message not received")
	}
}

func ExampleNewEncryptedData() {
	buffer := []byte("hello")
	message, err := dgproto.NewEncryptedData(1, 2, dgproto.TLV{Type: 1, Value: buffer})
	if err != nil {
		fmt.Println("invalid envelope")
		return
	}
	buffer[0] = 'X' // Original input can be reused, even if message is later queued.
	if err := message.ValidateFrameSize(16); err != nil {
		fmt.Println("invalid frame budget")
		return
	}
	fmt.Println(string(message.Fields[0].Value))
	// Send message using an existing Connection; keep message immutable while queued.
	// Output: hello
}
