package wire

import (
	"bytes"
	"testing"
)

func TestHeaderGoldenAndOwnership(t *testing.T) {
	h := Header{Magic: [4]byte{'D', 'G', 'P', '1'}, Version: 1, Flags: 0xfe, MessageType: 9, Reserved: [4]byte{0xa7, 0xb7, 0xb8, 0xb9}, SessionID: [16]byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}, Sequence: 0x0102030405060708, PayloadLength: 0x090a0b0c, PadLength: 255}
	want := []byte{0x44, 0x47, 0x50, 0x31, 1, 0xfe, 9, 0xa7, 0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 8, 7, 6, 5, 4, 3, 2, 1, 0x0c, 0x0b, 0x0a, 9, 255, 0xb7, 0xb8, 0xb9}
	buf := bytes.Repeat([]byte{0xcc}, HeaderSize+3)
	PutHeader(buf, h)
	if !bytes.Equal(buf[:HeaderSize], want) || !bytes.Equal(buf[HeaderSize:], []byte{0xcc, 0xcc, 0xcc}) {
		t.Fatalf("wire %x", buf)
	}
	got := ReadHeader(buf)
	if got != h {
		t.Fatalf("got %+v", got)
	}
	clear(buf)
	if got != h {
		t.Fatal("borrowed input")
	}
	PutHeader(buf, Header{})
	if !bytes.Equal(buf[:HeaderSize], make([]byte, HeaderSize)) {
		t.Fatal("zero header not fully written")
	}
}

func TestHeaderEveryOctetLossless(t *testing.T) {
	// Includes magic, unknown identifiers, every reserved byte and every endian
	// byte boundary; the primitive codec deliberately imposes no semantic policy.
	var src [HeaderSize + 1]byte
	var dst [HeaderSize + 1]byte
	for offset := 0; offset < HeaderSize; offset++ {
		for value := 0; value < 256; value++ {
			clear(src[:])
			src[offset] = byte(value)
			dst[HeaderSize] = 0xa5
			PutHeader(dst[:], ReadHeader(src[:]))
			if !bytes.Equal(dst[:HeaderSize], src[:HeaderSize]) || dst[HeaderSize] != 0xa5 {
				t.Fatalf("offset %d value %d", offset, value)
			}
		}
	}
}

func TestHeaderPreconditionsAndAllocations(t *testing.T) {
	for n := 0; n < HeaderSize; n++ {
		for _, write := range []bool{false, true} {
			func() {
				defer func() {
					if recover() == nil {
						t.Fatalf("short buffer %d write=%v did not panic", n, write)
					}
				}()
				buf := make([]byte, n)
				if write {
					PutHeader(buf, Header{})
				} else {
					_ = ReadHeader(buf)
				}
			}()
		}
	}
	var buf [HeaderSize]byte
	if got := testing.AllocsPerRun(1000, func() { PutHeader(buf[:], ReadHeader(buf[:])) }); got != 0 {
		t.Fatalf("allocations %g", got)
	}
}
