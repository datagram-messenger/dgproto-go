package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInvalidFlagValuesAreSafe(t *testing.T) {
	for _, args := range [][]string{
		{"-identity"}, {"-server-pin"}, {"-address"},
		{"-identity="}, {"-server-pin="}, {"-address="},
		{"-identity", "sensitive-input", "-server-pin", "sensitive-input", "extra"},
		{"-unknown=sensitive-input"},
	} {
		var out bytes.Buffer
		err := run(context.Background(), args, &out)
		if err == nil || out.Len() != 0 || strings.Contains(err.Error(), "sensitive-input") {
			t.Fatalf("invalid flags leaked input or succeeded")
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
		if !strings.HasPrefix(out.String(), "Usage: echo-client") || !strings.Contains(out.String(), "-server-pin") || strings.Contains(out.String(), "sensitive-input") {
			t.Fatalf("unexpected help: %q", out.String())
		}
	}
	want := errors.New("help output failure")
	if err := run(context.Background(), []string{"-help"}, failingWriter{want}); !errors.Is(err, want) {
		t.Fatalf("writer error lost: %v", err)
	}
}
