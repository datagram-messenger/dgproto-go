package dgproto_test

import (
	"fmt"

	dg "github.com/datagram-messenger/dgproto-go"
)

func ExampleEncryptedData_ValidateFrameSize() {
	message := dg.EncryptedData{
		StreamID:       1,
		AppMessageType: 2,
		Fields:         []dg.TLV{{Type: 1, Value: []byte("hello")}},
	}
	const padLength uint8 = 255
	if err := message.ValidateFrameSize(padLength); err != nil {
		fmt.Println(err)
		return
	}
	n, err := message.EncodedLen()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("payload bytes:", n)
	fmt.Println("frame bytes:", dg.HeaderSize+n+dg.AEADTagSize+int(padLength))
	// Before sending, keep the message and reachable buffers unchanged.
	// Preflight is not a snapshot, queue reservation or delivery guarantee.

	// Output:
	// payload bytes: 12
	// frame bytes: 323
}
