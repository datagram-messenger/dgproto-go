package dgproto_test

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"testing"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

var (
	_ encoding.BinaryMarshaler   = dgproto.PingPong{}
	_ encoding.BinaryMarshaler   = dgproto.Ack{}
	_ encoding.BinaryMarshaler   = dgproto.RekeyInit{}
	_ encoding.BinaryMarshaler   = dgproto.ResumptionTicket{}
	_ encoding.BinaryUnmarshaler = (*dgproto.PingPong)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.Ack)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.RekeyInit)(nil)
	_ encoding.BinaryUnmarshaler = (*dgproto.ResumptionTicket)(nil)
)

type messageSentinel struct{ label string }

func (e *messageSentinel) Error() string { return e.label }

func TestControlMessagePublicIdentity(t *testing.T) {
	for _, v := range []any{dgproto.PingPong{}, dgproto.Ack{}, dgproto.RekeyInit{}, dgproto.ResumptionTicket{}} {
		if got := reflect.TypeOf(v).PkgPath(); got != "github.com/datagram-messenger/dgproto-go" {
			t.Fatalf("%T has package %q", v, got)
		}
	}
}

func TestControlMessageCurrentSentinelsAndUnchangedOnError(t *testing.T) {
	// Never parallel: the public variables are intentionally replaceable.
	for _, tc := range []struct {
		name       string
		sentinel   *error
		input      []byte
		newValue   func() encoding.BinaryUnmarshaler
		diagnostic string
	}{
		{"ping length", &dgproto.ErrMessageLength, nil, func() encoding.BinaryUnmarshaler { return &dgproto.PingPong{IsResponse: true, Nonce: 9} }, ": got 0, want 9"},
		{"ping flag", &dgproto.ErrInvalidPingResponse, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0}, func() encoding.BinaryUnmarshaler { return &dgproto.PingPong{Nonce: 9} }, ": 0x02"},
		{"ack short", &dgproto.ErrMessageTooShort, nil, func() encoding.BinaryUnmarshaler { return &dgproto.Ack{Sequences: []uint64{9}} }, ""},
		{"ack count first", &dgproto.ErrAckCount, []byte{0, 1}, func() encoding.BinaryUnmarshaler { return &dgproto.Ack{Sequences: []uint64{9}} }, ": got 0"},
		{"ack length", &dgproto.ErrMessageLength, []byte{1}, func() encoding.BinaryUnmarshaler { return &dgproto.Ack{Sequences: []uint64{9}} }, ": got 1, want 9"},
		{"rekey length first", &dgproto.ErrMessageLength, nil, func() encoding.BinaryUnmarshaler { return &dgproto.RekeyInit{Epoch: 9} }, ": got 0, want 36"},
		{"rekey zero", &dgproto.ErrInvalidEpoch, make([]byte, 36), func() encoding.BinaryUnmarshaler { return &dgproto.RekeyInit{Epoch: 9} }, ""},
		{"ticket length", &dgproto.ErrResumptionTicket, make([]byte, 8), func() encoding.BinaryUnmarshaler { return &dgproto.ResumptionTicket{Ticket: []byte{9}, ValidUntil: 9} }, ": payload length 8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := *tc.sentinel
			defer func() { *tc.sentinel = original }()
			for _, sentinel := range []error{&messageSentinel{"replacement"}, nil} {
				*tc.sentinel = sentinel
				value := tc.newValue()
				before := fmt.Sprintf("%#v", value)
				err := value.UnmarshalBinary(tc.input)
				if tc.diagnostic == "" {
					if err != sentinel {
						t.Fatalf("direct sentinel = %v, want %v", err, sentinel)
					}
				} else {
					want := fmt.Errorf("%w%s", sentinel, tc.diagnostic)
					if err == nil || err.Error() != want.Error() {
						t.Fatalf("error = %v, want %v", err, want)
					}
				}
				if sentinel != nil {
					var typed *messageSentinel
					if !errors.Is(err, sentinel) || !errors.As(err, &typed) || typed != sentinel {
						t.Fatal("sentinel chain changed")
					}
				}
				if after := fmt.Sprintf("%#v", value); after != before {
					t.Fatalf("destination mutated: %s -> %s", before, after)
				}
			}
		})
	}
}

func TestControlMessageNilReceiversAndBoundaries(t *testing.T) {
	for _, tc := range controlBenchmarkCases() {
		wire, err := tc.message.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		var destination encoding.BinaryUnmarshaler
		switch tc.name {
		case "ping":
			destination = (*dgproto.PingPong)(nil)
		case "ack255":
			destination = (*dgproto.Ack)(nil)
		case "rekey":
			destination = (*dgproto.RekeyInit)(nil)
		case "ticket1024":
			destination = (*dgproto.ResumptionTicket)(nil)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s valid input to nil receiver must panic", tc.name)
				}
			}()
			destination.UnmarshalBinary(wire)
		}()
		if err := destination.UnmarshalBinary(nil); err == nil {
			t.Errorf("%s short input to nil receiver must return error", tc.name)
		}
	}
	for _, tc := range []struct {
		message encoding.BinaryMarshaler
		want    error
	}{
		{dgproto.Ack{}, dgproto.ErrAckCount},
		{dgproto.Ack{Sequences: []uint64{}}, dgproto.ErrAckCount},
		{dgproto.Ack{Sequences: make([]uint64, 256)}, dgproto.ErrAckCount},
		{dgproto.RekeyInit{}, dgproto.ErrInvalidEpoch},
		{dgproto.ResumptionTicket{}, dgproto.ErrResumptionTicket},
		{dgproto.ResumptionTicket{Ticket: []byte{}}, dgproto.ErrResumptionTicket},
		{dgproto.ResumptionTicket{Ticket: make([]byte, dgproto.MaxResumptionTicketSize+1)}, dgproto.ErrResumptionTicket},
	} {
		got, err := tc.message.MarshalBinary()
		if got != nil || !errors.Is(err, tc.want) {
			t.Fatalf("%T invalid encode = %v, %v", tc.message, got, err)
		}
	}
	for _, size := range []int{1, dgproto.MaxResumptionTicketSize} {
		value := dgproto.ResumptionTicket{Ticket: make([]byte, size), ValidUntil: ^uint64(0)}
		wire, err := value.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		var got dgproto.ResumptionTicket
		if err := got.UnmarshalBinary(wire); err != nil || !reflect.DeepEqual(got, value) {
			t.Fatalf("ticket boundary %d: %v", size, err)
		}
	}
}

func TestControlMessageOwnershipAndAllocations(t *testing.T) {
	ack := dgproto.Ack{Sequences: []uint64{1, 2}}
	encoded, err := ack.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded dgproto.Ack
	if err := decoded.UnmarshalBinary(encoded); err != nil {
		t.Fatal(err)
	}
	encoded[1] = 99
	ack.Sequences[0] = 88
	if decoded.Sequences[0] != 1 {
		t.Fatal("ack aliases input")
	}
	ticket := dgproto.ResumptionTicket{Ticket: []byte{1, 2}, ValidUntil: 7}
	encoded, err = ticket.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var got dgproto.ResumptionTicket
	if err := got.UnmarshalBinary(encoded); err != nil {
		t.Fatal(err)
	}
	encoded[0], ticket.Ticket[0] = 99, 88
	if !bytes.Equal(got.Ticket, []byte{1, 2}) {
		t.Fatal("ticket aliases input")
	}
	for _, tc := range controlBenchmarkCases() {
		wire, err := tc.message.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if len(wire) != cap(wire) {
			t.Fatalf("%s len/cap changed", tc.name)
		}
		allocs := testing.AllocsPerRun(100, func() { controlBytes, controlErr = tc.message.MarshalBinary() })
		if allocs != 1 || controlErr != nil {
			t.Fatalf("%s encode allocations = %v, error = %v", tc.name, allocs, controlErr)
		}
		allocs = testing.AllocsPerRun(100, func() { controlErr = tc.destination.UnmarshalBinary(wire) })
		want := float64(0)
		if tc.name == "ack255" || tc.name == "ticket1024" {
			want = 1
		}
		if allocs != want || controlErr != nil {
			t.Fatalf("%s decode allocations = %v, want %v, error = %v", tc.name, allocs, want, controlErr)
		}
	}
}

var controlBytes []byte
var controlErr error

type controlBenchmarkCase struct {
	name        string
	message     encoding.BinaryMarshaler
	destination encoding.BinaryUnmarshaler
}

func controlBenchmarkCases() []controlBenchmarkCase {
	sequences := make([]uint64, 255)
	for i := range sequences {
		sequences[i] = uint64(i) + 1
	}
	return []controlBenchmarkCase{
		{"ping", dgproto.PingPong{IsResponse: true, Nonce: 7}, &dgproto.PingPong{}},
		{"ack255", dgproto.Ack{Sequences: sequences}, &dgproto.Ack{}},
		{"rekey", dgproto.RekeyInit{Epoch: 2, KeyConfirm: [32]byte{1}}, &dgproto.RekeyInit{}},
		{"ticket1024", dgproto.ResumptionTicket{Ticket: make([]byte, 1024), ValidUntil: 7}, &dgproto.ResumptionTicket{}},
	}
}

func BenchmarkControlMessageBoundary(b *testing.B) {
	for _, tc := range controlBenchmarkCases() {
		wire, err := tc.message.MarshalBinary()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(tc.name+"/encode", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				controlBytes, controlErr = tc.message.MarshalBinary()
				if controlErr != nil {
					b.Fatal(controlErr)
				}
			}
		})
		b.Run(tc.name+"/decode", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := tc.destination.UnmarshalBinary(wire); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
