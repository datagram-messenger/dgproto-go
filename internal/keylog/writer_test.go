//go:build dgproto_keylog

package keylog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Tests are serial: production initialization and its file are process-wide.
func resetSink(t *testing.T) {
	t.Helper()
	reset := func() {
		if writer != nil {
			if err := writer.Close(); err != nil {
				t.Error(err)
			}
		}
		once = sync.Once{}
		mu = sync.Mutex{}
		writer = nil
		active = false
	}
	reset()
	t.Cleanup(reset)
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func TestRecordFormatAppendAndSingleOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.log")
	other := filepath.Join(t.TempDir(), "other.log")
	resetSink(t) // Register close after TempDir cleanup (cleanup runs LIFO).
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DGPROTO_KEYLOG", path)
	var sid [16]byte
	var send, recv [32]byte
	for i := range sid {
		sid[i] = byte(i)
	}
	for i := range send {
		send[i] = byte(i + 16)
		recv[i] = byte(i + 48)
	}
	Write(sid, send, recv)
	t.Setenv("DGPROTO_KEYLOG", other)
	WriteEpoch(sid, "recv", 7, recv)
	want := "existing\n" +
		"DGPROTO_SESSION_KEY 000102030405060708090a0b0c0d0e0f send 1 101112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f\n" +
		"DGPROTO_SESSION_KEY 000102030405060708090a0b0c0d0e0f recv 1 303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f\n" +
		"DGPROTO_SESSION_KEY 000102030405060708090a0b0c0d0e0f recv 7 303132333435363738393a3b3c3d3e3f404142434445464748494a4b4c4d4e4f\n"
	if got := readLog(t, path); got != want {
		t.Fatalf("record bytes differ: got %q, want %q", got, want)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatalf("second path stat = %v, want absent", err)
	}
}

func TestDisabledAndFailedOpenDoNotRetry(t *testing.T) {
	for _, mode := range []string{"unset", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			resetSink(t)
			path := ""
			if mode == "invalid" {
				path = t.TempDir() // Opening a directory for append must fail.
			}
			t.Setenv("DGPROTO_KEYLOG", path)
			WriteEpoch([16]byte{}, "send", 2, [32]byte{})
			valid := filepath.Join(t.TempDir(), "keys.log")
			t.Setenv("DGPROTO_KEYLOG", valid)
			Write([16]byte{}, [32]byte{}, [32]byte{})
			if _, err := os.Stat(valid); !os.IsNotExist(err) {
				t.Fatalf("retry path stat = %v, want absent", err)
			}
		})
	}
}

func TestConcurrentInitialPairsAndEpochRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.log")
	resetSink(t)
	t.Setenv("DGPROTO_KEYLOG", path)
	const callers = 32
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := [16]byte{byte(i)}
			Write(sid, [32]byte{1}, [32]byte{2})
			WriteEpoch(sid, "send", 2, [32]byte{3})
		}(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(readLog(t, path), "\n"), "\n")
	if len(lines) != callers*3 {
		t.Fatalf("line count = %d, want %d", len(lines), callers*3)
	}
	counts := make(map[string]int)
	for i := 0; i < len(lines); i++ {
		fields := strings.Fields(lines[i])
		if len(fields) != 5 || fields[0] != "DGPROTO_SESSION_KEY" {
			t.Fatalf("malformed line %d", i)
		}
		counts[fields[1]+" "+fields[2]+" "+fields[3]]++
		if fields[2] == "send" && fields[3] == "1" {
			if i+1 >= len(lines) {
				t.Fatal("missing paired receive line")
			}
			next := strings.Fields(lines[i+1])
			if len(next) != 5 || next[1] != fields[1] || next[2] != "recv" || next[3] != "1" {
				t.Fatal("initial key pair interleaved")
			}
		}
	}
	for i := 0; i < callers; i++ {
		sid := fmt.Sprintf("%x", [16]byte{byte(i)})
		for _, suffix := range []string{" send 1", " recv 1", " send 2"} {
			if counts[sid+suffix] != 1 {
				t.Fatalf("record count for caller %d %s = %d", i, suffix, counts[sid+suffix])
			}
		}
	}
}
