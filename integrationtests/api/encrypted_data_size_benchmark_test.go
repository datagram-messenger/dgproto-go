package dgproto_test

import (
	"testing"

	dg "github.com/datagram-messenger/dgproto-go"
)

var encryptedSizeBenchmarkLen int
var encryptedSizeBenchmarkWire []byte

func BenchmarkEncryptedDataSizePreflight(b *testing.B) {
	for _, shape := range []struct {
		name     string
		fields   int
		valueLen int
	}{
		{"empty", 0, 0},
		{"small", 4, 17},
		{"all-types", 256, 17},
		{"max-pad255", 1, 65217},
	} {
		m := dg.EncryptedData{Fields: make([]dg.TLV, shape.fields)}
		for i := range m.Fields {
			m.Fields[i] = dg.TLV{Type: uint8(i), Value: make([]byte, shape.valueLen)}
		}
		b.Run(shape.name, func(b *testing.B) {
			b.Run("EncodedLen", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					n, err := m.EncodedLen()
					if err != nil {
						b.Fatal(err)
					}
					encryptedSizeBenchmarkLen = n
				}
			})
			b.Run("ValidateFrameSize", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if err := m.ValidateFrameSize(255); err != nil {
						b.Fatal(err)
					}
				}
			})
			// Same-input reference, not a claim of a before/after encoder speedup.
			b.Run("MarshalBinary", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					wire, err := m.MarshalBinary()
					if err != nil {
						b.Fatal(err)
					}
					encryptedSizeBenchmarkWire = wire
				}
			})
		})
	}
}
