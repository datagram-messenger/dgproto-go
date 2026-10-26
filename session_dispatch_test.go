package dgproto

import "testing"

func TestMessageType09IsError(t *testing.T) {
	if MessageTypeError != 0x09 {
		t.Fatalf("error type = %#x", MessageTypeError)
	}
	message, err := newInboundMessage(0x09)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := message.(*ErrorMessage); !ok {
		t.Fatalf("0x09 decoded as %T", message)
	}
}
