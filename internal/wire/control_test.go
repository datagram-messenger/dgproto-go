package wire

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestControlGolden(t *testing.T) {
	ping := make([]byte, 10)
	ping[9] = 0xa5
	PutPingPong(ping, true, 0x0807060504030201)
	if got := hex.EncodeToString(ping); got != "010102030405060708a5" {
		t.Fatal(got)
	}
	response, nonce := ReadPingPong(ping[:9])
	if !response || nonce != 0x0807060504030201 {
		t.Fatal(response, nonce)
	}
	PutPingPong(ping, false, 0)
	if !bytes.Equal(ping[:9], make([]byte, 9)) || ping[9] != 0xa5 {
		t.Fatal("zero overwrite/suffix")
	}

	ack := make([]byte, 18)
	ack[17] = 0xa5
	sequences := []uint64{0x0807060504030201, 0x100f0e0d0c0b0a09}
	PutAck(ack, sequences)
	if got := hex.EncodeToString(ack); got != "020102030405060708090a0b0c0d0e0f10a5" {
		t.Fatal(got)
	}
	gotSequences := make([]uint64, 2)
	ReadAck(ack[:17], gotSequences)
	if !reflect.DeepEqual(gotSequences, sequences) {
		t.Fatal(gotSequences)
	}

	var confirm [32]byte
	for i := range confirm {
		confirm[i] = byte(i)
	}
	rekey := make([]byte, 37)
	rekey[36] = 0xa5
	PutRekeyInit(rekey, 0x04030201, confirm)
	if got := hex.EncodeToString(rekey); got != "01020304000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1fa5" {
		t.Fatal(got)
	}
	epoch, gotConfirm := ReadRekeyInit(rekey[:36])
	if epoch != 0x04030201 || gotConfirm != confirm {
		t.Fatal(epoch, gotConfirm)
	}
	PutRekeyInit(rekey, 0, [32]byte{})
	if !bytes.Equal(rekey[:36], make([]byte, 36)) || rekey[36] != 0xa5 {
		t.Fatal("rekey zero overwrite/suffix")
	}

	ticket := make([]byte, 11)
	ticket[10] = 0xa5
	PutTicket(ticket, []byte{0x11, 0x22}, 0x0807060504030201)
	if got := hex.EncodeToString(ticket); got != "11220102030405060708a5" {
		t.Fatal(got)
	}
	view, expiration := ReadTicket(ticket[:10])
	if !bytes.Equal(view, []byte{0x11, 0x22}) || expiration != 0x0807060504030201 {
		t.Fatal(view, expiration)
	}
	view[0] = 0x33
	if ticket[0] != 0x33 {
		t.Fatal("internal ticket view should borrow input")
	}
}

func TestAckAllCountsAndOctets(t *testing.T) {
	for count := 1; count <= MaxAckSequences; count++ {
		sequences := make([]uint64, count)
		for i := range sequences {
			sequences[i] = uint64(i) * 0x0101010101010101
		}
		buf := make([]byte, AckSize(count)+1)
		buf[len(buf)-1] = 0xa5
		PutAck(buf, sequences)
		got := make([]uint64, count)
		ReadAck(buf[:len(buf)-1], got)
		if buf[0] != byte(count) || buf[len(buf)-1] != 0xa5 || !reflect.DeepEqual(got, sequences) {
			t.Fatalf("count %d", count)
		}
	}
}

func TestControlPrimitiveAllocations(t *testing.T) {
	var ping [9]byte
	var rekey [36]byte
	var ack [17]byte
	var ticket [10]byte
	sequences := []uint64{1, 2}
	out := make([]uint64, 2)
	if got := testing.AllocsPerRun(100, func() {
		PutPingPong(ping[:], true, 9)
		ReadPingPong(ping[:])
		PutRekeyInit(rekey[:], 2, [32]byte{1})
		ReadRekeyInit(rekey[:])
		PutAck(ack[:], sequences)
		ReadAck(ack[:], out)
		PutTicket(ticket[:], []byte{1, 2}, 9)
		ReadTicket(ticket[:])
	}); got != 0 {
		t.Fatalf("allocations = %v", got)
	}
}

func TestControlShortBufferPreconditions(t *testing.T) {
	for _, call := range []func(){
		func() { PutPingPong(make([]byte, 8), false, 0) },
		func() { ReadPingPong(make([]byte, 8)) },
		func() { PutAck(make([]byte, 8), []uint64{1}) },
		func() { ReadAck(make([]byte, 8), make([]uint64, 1)) },
		func() { PutRekeyInit(make([]byte, 35), 1, [32]byte{}) },
		func() { ReadRekeyInit(make([]byte, 35)) },
		func() { PutTicket(make([]byte, 8), []byte{1}, 9) },
		func() { ReadTicket(make([]byte, 8)) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("missing precondition panic")
				}
			}()
			call()
		}()
	}
}
