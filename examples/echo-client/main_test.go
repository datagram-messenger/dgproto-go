package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestInvalidOptionsProduceNoOutput(t *testing.T) {
	for _, args := range [][]string{nil, {"-identity", "private-path"}, {"-server-pin", "pin-path"}, {"-unknown", "sensitive-input"}, {"extra"}, {"-identity", "missing-private", "-server-pin", "missing-pin"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err == nil {
			t.Fatal("invalid input accepted")
		}
		if out.Len() != 0 {
			t.Fatal("failure produced success output")
		}
	}
}

func TestCanceledBeforeLoadingInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := run(ctx, nil, &out); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("canceled client produced output")
	}
}
