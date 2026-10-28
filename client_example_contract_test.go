package dgproto_test

import (
	dgproto "github.com/datagram-messenger/dgproto-go"
	"testing"
)

func TestPinnedExampleEnvelope(t *testing.T) {
	for _, data := range []*dgproto.EncryptedData{
		nil,
		{StreamID: 2, AppMessageType: 2},
		{StreamID: 1, AppMessageType: 1},
		{StreamID: 1, AppMessageType: 2, Fields: []dgproto.TLV{{Type: 1}}},
	} {
		if validatePinnedEnvelope(data, 2) == nil {
			t.Fatal("invalid reply accepted")
		}
	}
	if err := validatePinnedEnvelope(&dgproto.EncryptedData{StreamID: 1, AppMessageType: 2}, 2); err != nil {
		t.Fatal(err)
	}
}
