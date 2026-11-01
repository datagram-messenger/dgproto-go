package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInvalidFlagValuesAreSafe(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-identity"}, "invalid server options"},
		{[]string{"-allowed-client"}, "invalid server options"},
		{[]string{"-listen"}, "invalid server options"},
		{[]string{"-identity=", "-allowed-client", "sensitive-input"}, "missing server options"},
		{[]string{"-identity", "sensitive-input", "-allowed-client="}, "missing server options"},
		{[]string{"-identity", "sensitive-input", "-allowed-client", "sensitive-input", "-listen="}, "missing server options"},
		{[]string{"-identity", "sensitive-input", "-allowed-client", "sensitive-input", "extra"}, "missing server options"},
		{[]string{"-unknown=sensitive-input"}, "invalid server options"},
	} {
		var out bytes.Buffer
		err := run(context.Background(), tc.args, &out)
		// Reject flags before reading provisioned identities or listening.
		if err == nil || err.Error() != tc.want || out.Len() != 0 {
			t.Fatal("invalid flags were not rejected before loading inputs")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := run(ctx, []string{"-help"}, &out); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatal("canceled help performed output")
	}
}

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
