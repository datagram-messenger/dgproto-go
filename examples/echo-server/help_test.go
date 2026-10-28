package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestHelpIsSafe(t *testing.T) {
	for _, flag := range []string{"-h", "-help"} {
		var out bytes.Buffer
		if err := run(context.Background(), []string{flag, "sensitive-input"}, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), "Usage: echo-server") || !strings.Contains(out.String(), "-allowed-client") || strings.Contains(out.String(), "sensitive-input") {
			t.Fatalf("unexpected help: %q", out.String())
		}
	}
	want := errors.New("help output failure")
	writer := startupWriter(func([]byte) (int, error) { return 0, want })
	if err := run(context.Background(), []string{"-help"}, writer); !errors.Is(err, want) {
		t.Fatalf("writer error lost: %v", err)
	}
}
