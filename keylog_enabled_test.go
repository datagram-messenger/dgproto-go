//go:build dgproto_keylog

package dgproto

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestKeyLogRecordsCommittedRekeyEpochs(t *testing.T) {
	path := t.TempDir() + "/keys.log"
	keylogOnce = sync.Once{}
	keylogWriter = nil
	keylogActive = false
	t.Setenv("DGPROTO_KEYLOG", path)
	t.Cleanup(func() {
		if keylogWriter != nil {
			_ = keylogWriter.Close()
		}
		keylogOnce = sync.Once{}
		keylogWriter = nil
		keylogActive = false
	})

	sender, receiver := testSessions(t)
	sender.rekeyFrameLimit = 1
	if _, err := sender.Send(PingPong{Nonce: 1}, 0); err != nil {
		t.Fatal(err)
	}
	frame, err := sender.Send(PingPong{Nonce: 2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Header.MessageType != MessageTypeRekeyInit {
		t.Fatalf("message type = 0x%02x, want rekey", frame.Header.MessageType)
	}
	if err := sender.MarkRekeySent(frame); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Receive(frame); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	log := string(contents)
	if !strings.Contains(log, " send 2 ") {
		t.Fatalf("key log missing send epoch 2:\n%s", log)
	}
	if !strings.Contains(log, " recv 2 ") {
		t.Fatalf("key log missing receive epoch 2:\n%s", log)
	}
}
