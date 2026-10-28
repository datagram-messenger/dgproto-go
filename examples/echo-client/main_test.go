package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
	"github.com/datagram-messenger/dgproto-go/examples/internal/echoapp"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestProvisionedCommandRoundTripAndOutputFailure(t *testing.T) {
	private := bytes.Repeat([]byte{11}, 32) // Synthetic, never a deployment identity.
	client, err := dgproto.LoadStaticKey(private)
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identity, pin := filepath.Join(dir, "client.key"), filepath.Join(dir, "server.pub")
	if err := os.WriteFile(identity, private, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pin, serverKey.Public(), 0600); err != nil {
		t.Fatal(err)
	}
	server, err := echoapp.NewServer(serverKey, client.Public())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- echoapp.Serve(ctx, server, listener) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server failed to join")
		}
	}()
	args := []string{"-address", listener.Addr().String(), "-identity", identity, "-server-pin", pin}
	var out bytes.Buffer
	if err := run(ctx, args, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "authenticated application echo received\n" {
		t.Fatal("incorrect command output")
	}
	outputErr := errors.New("test output failure")
	if err := run(ctx, args, failingWriter{outputErr}); !errors.Is(err, outputErr) {
		t.Fatalf("output failure lost: %v", err)
	}
}

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
