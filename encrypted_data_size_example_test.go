package dgproto_test

import (
	"fmt"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

func ExampleEncryptedData_EncodedLen() {
	message := dgproto.EncryptedData{
		Fields: []dgproto.TLV{{Type: 1, Value: []byte("hello")}},
	}
	n, err := message.EncodedLen()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(n) // 4-byte envelope + 8-byte aligned TLV; not a frame size.
	// Use ValidateFrameSize with the intended padding before enqueueing.
	// Keep inputs unchanged: neither method snapshots or reserves anything,
	// and neither promises a successful send or delivery.

	// Output: 12
}
