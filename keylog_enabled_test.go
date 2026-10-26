//go:build dgproto_keylog

package dgproto

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestKeyLogRecordsCommittedRekeyEpochs(t *testing.T) {
	// The sink is process-wide and intentionally has no reset/close API.
	// Isolate initialization instead of reaching into its private state.
	if os.Getenv("DGPROTO_KEYLOG_REKEY_TEST") != "1" {
		path := t.TempDir() + "/keys.log"
		cmd := exec.Command(os.Args[0], "-test.run=^TestKeyLogRecordsCommittedRekeyEpochs$", "-test.timeout=15s")
		cmd.Env = append(os.Environ(), "DGPROTO_KEYLOG_REKEY_TEST=1", "DGPROTO_KEYLOG="+path)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated rekey test: %v\n%s", err, output)
		}
		return
	}
	path := os.Getenv("DGPROTO_KEYLOG")

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
	pending, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pending), " send 2 ") || strings.Contains(string(pending), " recv 2 ") {
		t.Fatal("pending rekey logged before commit")
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
	// NewSession logs the initial epoch-1 keys.
	if !strings.Contains(log, " send 1 ") {
		t.Fatalf("key log missing initial send epoch 1:\n%s", log)
	}
	if !strings.Contains(log, " recv 1 ") {
		t.Fatalf("key log missing initial receive epoch 1:\n%s", log)
	}
	// MarkRekeySent and Receive(RekeyInit) log the committed epoch-2 keys.
	if !strings.Contains(log, " send 2 ") {
		t.Fatalf("key log missing send epoch 2:\n%s", log)
	}
	if !strings.Contains(log, " recv 2 ") {
		t.Fatalf("key log missing receive epoch 2:\n%s", log)
	}
}
