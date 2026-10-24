package dgproto

import (
	"bytes"
	"testing"
)

func BenchmarkDecodeTLVShapes(b *testing.B) {
	shapes := []struct {
		name string
		wire []byte
	}{
		{"empty", nil}, {"tiny", []byte{1, 1, 0, 7}},
		{"few", bytes.Repeat([]byte{1, 1, 0, 7}, 4)},
		{"manyempty", bytes.Repeat([]byte{1, 0, 0, 0}, MaxTLVElements)},
		{"malformed", []byte{1, 255, 255, 0}},
	}
	for _, size := range []struct {
		name string
		n    int
	}{{"longsingle", 64000}, {"maxsingle", 65529}} {
		wire, err := (TLV{Type: 1, Value: make([]byte, size.n)}).MarshalBinary()
		if err != nil {
			b.Fatal(err)
		}
		shapes = append(shapes, struct {
			name string
			wire []byte
		}{size.name, wire})
	}
	for _, shape := range shapes {
		b.Run(shape.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out, err := DecodeTLVs(shape.wire, 0)
				if shape.name == "malformed" {
					if err == nil || out != nil {
						b.Fatal("expected atomic error")
					}
				} else if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
