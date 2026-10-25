package tlv

import (
	"bytes"
	"math"
	"testing"
)

// Relocated from root: these assertions exercise only wire encoding/alignment.
func TestTLVGoldenWire(t *testing.T) {
	got, failure := Marshal(0x2a, []byte{0x10, 0x20, 0x30})
	want := []byte{0x2a, 0x03, 0x00, 0x10, 0x20, 0x30, 0x00, 0x00}
	if failure.Kind != OK || !bytes.Equal(got, want) {
		t.Fatalf("Marshal() = % x, %+v, want % x", got, failure, want)
	}
}

func TestTLVAlignmentAndEmptyValue(t *testing.T) {
	for valueLen, wantLen := range map[int]int{0: 4, 1: 4, 2: 8, 3: 8, 4: 8, 5: 8, 6: 12, 7: 12, 8: 12} {
		t.Run(string(rune('A'+valueLen)), func(t *testing.T) {
			got, failure := Marshal(1, make([]byte, valueLen))
			if failure.Kind != OK || len(got) != wantLen {
				t.Fatalf("encoded length for value %d = %d, want %d", valueLen, len(got), wantLen)
			}
		})
	}
}

func TestCodecWireAndBorrowedRead(t *testing.T) {
	for size := 0; size <= 12; size++ {
		value := bytes.Repeat([]byte{0xa5}, size)
		wire, failure := Marshal(0xfe, value)
		if failure.Kind != OK || len(wire) != Align4(HeaderSize+size) || wire[0] != 0xfe || wire[1] != byte(size) || wire[2] != 0 {
			t.Fatalf("size %d: %x, %+v", size, wire, failure)
		}
		if !bytes.Equal(wire[3:3+size], value) || !bytes.Equal(wire[3+size:], make([]byte, len(wire)-3-size)) {
			t.Fatal("value or zero padding changed")
		}
		for i := 3 + size; i < len(wire); i++ {
			wire[i] = 0xff // alignment bytes are opaque on receive
		}
		count, failure := Validate(wire, 0)
		typ, borrowed, next := Read(wire, 0)
		if count != 1 || failure.Kind != OK || typ != 0xfe || next != len(wire) || !bytes.Equal(borrowed, value) {
			t.Fatalf("second pass: %d, %+v", count, failure)
		}
		if size > 0 {
			borrowed[0] = 7
			if wire[3] != 7 || value[0] != 0xa5 {
				t.Fatal("Read must borrow wire; Marshal must own output")
			}
		}
	}
}

func TestValidationAtomicBoundariesAndPrecedence(t *testing.T) {
	prefix := []byte{0xfe, 1, 0, 7, 0xfe, 0, 0, 0xa5}
	for _, tc := range []struct {
		wire  []byte
		limit int
		want  Failure
	}{
		{[]byte{1}, 0, Failure{Kind: TooShort, Got: 1}},
		{[]byte{1, 255, 255}, 0, Failure{Kind: TruncatedValue, Need: 65535}},
		{[]byte{1, 2, 0, 7, 8}, 0, Failure{Kind: TruncatedPadding, Need: 3}},
		{append(append([]byte(nil), prefix...), 1), 0, Failure{Kind: TooShort, Offset: 8, Got: 1}},
		{make([]byte, MaxSequenceSize+1), 1, Failure{Kind: SequenceTooLarge, Got: MaxSequenceSize + 1, Limit: MaxSequenceSize}},
		{[]byte{1}, 1, Failure{Kind: TooShort, Got: 1}},
		{[]byte{1, 2}, 1, Failure{Kind: DecodeLimit, Got: 2, Limit: 1}},
		{append(bytes.Repeat([]byte{1, 0, 0, 0}, MaxElements), 1), 0, Failure{Kind: ElementsFull, Limit: MaxElements}},
	} {
		count, got := Validate(tc.wire, tc.limit)
		if count != 0 || got != tc.want {
			t.Fatalf("count=%d failure=%+v, want %+v", count, got, tc.want)
		}
	}
	for _, limit := range []int{0, -1, math.MinInt, MaxSequenceSize} {
		wire := bytes.Repeat([]byte{1, 0, 0, 0xff}, MaxElements)
		if count, failure := Validate(wire, limit); count != MaxElements || failure.Kind != OK {
			t.Fatalf("dense boundary: %d %+v", count, failure)
		}
	}
	wire, _ := Marshal(7, make([]byte, 65529))
	for n := 1; n < len(wire); n++ {
		if count, failure := Validate(wire[:n], 0); count != 0 || failure.Kind == OK {
			t.Fatalf("truncation %d accepted", n)
		}
	}
}

func TestBoundedSizeArithmetic(t *testing.T) {
	for _, n := range []int{MaxValueSize + 1, math.MaxInt} {
		if size, failure := EncodedLen(n); size != 0 || failure.Kind != ValueTooLarge || failure.Got != n {
			t.Fatalf("overflow value %d: %d %+v", n, size, failure)
		}
	}
	if size, failure := EncodedLen(MaxValueSize); size != 65540 || failure.Kind != OK {
		t.Fatalf("single value length: %d %+v", size, failure)
	}
	if size, failure := AddLen(0, 65529); size != 65532 || failure != OK {
		t.Fatal("maximum fitting single value rejected")
	}
	if _, failure := AddLen(65532, 0); failure != SequenceFull {
		t.Fatal("sequence overflow accepted")
	}
	if _, failure := AddLen(65532, MaxValueSize+1); failure != ValueTooLarge {
		t.Fatal("value failure must precede sequence failure")
	}
	if CheckCount(MaxElements).Kind != OK || CheckCount(MaxElements+1).Kind != TooManyElements {
		t.Fatal("element bounds changed")
	}
}

func TestValidationDoesNotAllocate(t *testing.T) {
	for _, wire := range [][]byte{nil, {1, 1, 0, 7}, {1, 255, 255}, bytes.Repeat([]byte{1, 0, 0, 0}, MaxElements)} {
		if got := testing.AllocsPerRun(100, func() { Validate(wire, 0) }); got != 0 {
			t.Fatalf("Validate allocated %g times", got)
		}
	}
}
