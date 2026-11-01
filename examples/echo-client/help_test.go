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
		{[]string{"-identity"}, "invalid client options"},
		{[]string{"-server-pin"}, "invalid client options"},
		{[]string{"-address"}, "invalid client options"},
		{[]string{"-identity=", "-server-pin", "sensitive-input"}, "missing client options"},
		{[]string{"-identity", "sensitive-input", "-server-pin="}, "missing client options"},
		{[]string{"-identity", "sensitive-input", "-server-pin", "sensitive-input", "-address="}, "missing client options"},
		{[]string{"-identity", "sensitive-input", "-server-pin", "sensitive-input", "extra"}, "missing client options"},
		{[]string{"-unknown=sensitive-input"}, "invalid client options"},
	} {
		var out bytes.Buffer
		err := run(context.Background(), tc.args, &out)
		// With all other required flags supplied, exact option errors prove
		// rejection happens before attempting to load either input file.
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
		if !strings.HasPrefix(out.String(), "Usage: echo-client") || !strings.Contains(out.String(), "-server-pin") || strings.Contains(out.String(), "sensitive-input") {
			t.Fatalf("unexpected help: %q", out.String())
		}
	}
	want := errors.New("help output failure")
	if err := run(context.Background(), []string{"-help"}, failingWriter{want}); !errors.Is(err, want) {
		t.Fatalf("writer error lost: %v", err)
	}
}
