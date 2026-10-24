package dgproto

import (
	"bytes"
	"errors"
	"testing"
)

// referenceEncodeTLVs retains the original per-field encoding for equivalence checks.
func referenceEncodeTLVs(fields []TLV) ([]byte, error) {
	if len(fields) > MaxTLVElements {
		return nil, ErrTLVElementLimit
	}
	total := 0
	for _, field := range fields {
		n, err := field.EncodedLen()
		if err != nil {
			return nil, err
		}
		if n > MaxTLVSequenceSize-total {
			return nil, ErrTLVSequenceLimit
		}
		total += n
	}
	out := make([]byte, 0, total)
	for _, field := range fields {
		data, err := field.MarshalBinary()
		if err != nil {
			return nil, err
		}
		out = append(out, data...)
	}
	return out, nil
}

func TestEncodeTLVsEquivalentEdgesAndOwnership(t *testing.T) {
	cases := [][]TLV{nil, {}, {{Type: 255}}, {{Type: 1, Value: []byte{1}}, {Type: 1, Value: []byte{2, 3}}, {Type: 255, Value: []byte{4, 5, 6, 7}}}, {{Type: 7, Value: make([]byte, 65529)}}, {{Value: make([]byte, MaxTLVValueSize)}}, {{Value: make([]byte, MaxTLVValueSize+1)}}, make([]TLV, MaxTLVElements), make([]TLV, MaxTLVElements+1)}
	for n := 0; n < 9; n++ {
		cases = append(cases, []TLV{{Type: 7, Value: bytes.Repeat([]byte{0xa5}, n)}})
	}
	cases = append(cases, []TLV{{Value: make([]byte, 65529)}, {Value: make([]byte, MaxTLVValueSize+1)}})
	for i, fields := range cases {
		want, wantErr := referenceEncodeTLVs(fields)
		got, err := EncodeTLVs(fields)
		for _, sentinel := range []error{ErrTLVElementLimit, ErrTLVSequenceLimit, ErrTLVValueTooLarge} {
			if errors.Is(err, sentinel) != errors.Is(wantErr, sentinel) {
				t.Fatalf("case %d: errors %v / %v", i, err, wantErr)
			}
		}
		if (err == nil) != (wantErr == nil) || !bytes.Equal(got, want) || (got == nil) != (want == nil) {
			t.Fatalf("case %d differs", i)
		}
	}
	input := []byte{1, 2, 3, 4}
	fields := []TLV{{Type: 1, Value: input}, {Type: 1, Value: input[:2]}}
	got, err := EncodeTLVs(fields)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte(nil), got...)
	input[0] = 9
	if !bytes.Equal(got, want) {
		t.Fatal("output aliases input")
	}
	got[4] = 8
	if input[1] != 2 {
		t.Fatal("input aliases output")
	}
	again, err := EncodeTLVs(fields)
	if err != nil {
		t.Fatal(err)
	}
	again[4] = 7
	if got[4] != 8 {
		t.Fatal("outputs alias each other")
	}
}

var tlvEncodeSink []byte

func BenchmarkEncodeTLVsPatch(b *testing.B) {
	for _, tc := range []struct {
		name   string
		fields []TLV
	}{
		{"Long", []TLV{{Type: 1, Value: make([]byte, 60000)}}},
		{"Typical", []TLV{{Type: 1, Value: make([]byte, 8)}, {Type: 2, Value: make([]byte, 32)}, {Type: 3, Value: make([]byte, 128)}}},
		{"ManyEmpty", make([]TLV, 4096)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				tlvEncodeSink, err = EncodeTLVs(tc.fields)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
