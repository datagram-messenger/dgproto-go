package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExternalConsumerExampleCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the public-only loopback example in an external module")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir() // Never create a nested go.mod in the checkout.
	source, err := os.ReadFile(filepath.Join(root, "examples", "echo", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	quotedRoot, err := json.Marshal(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	module := "module example.invalid/dgproto-consumer\n\ngo 1.25.0\n\nrequire github.com/datagram-messenger/dgproto-go v0.0.0\n\nreplace github.com/datagram-messenger/dgproto-go => " + string(quotedRoot) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), source, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "-o", filepath.Join(dir, "consumer.exe"), "."}} {
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir, cmd.Env, cmd.WaitDelay = dir, smokeEnvironment(), 2*time.Second
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Run(); err != nil {
			t.Fatalf("external consumer go %s failed: %v", args[0], err)
		}
	}
}
