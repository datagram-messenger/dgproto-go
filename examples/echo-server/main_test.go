package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
	"github.com/datagram-messenger/dgproto-go/examples/internal/echoapp"
)

// Provision only synthetic fixtures; never read developer identity files.
func provisionServer(t *testing.T) ([]string, dgproto.StaticKey, dgproto.StaticKey) {
	t.Helper()
	private := make([]byte, 32)
	if _, err := rand.Read(private); err != nil {
		t.Fatal(err)
	}
	defer clear(private)
	server, err := dgproto.LoadStaticKey(private)
	if err != nil {
		t.Fatal(err)
	}
	client, err := dgproto.GenerateStaticKey()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identity, allowed := filepath.Join(dir, "server.key"), filepath.Join(dir, "client.pub")
	if err := os.WriteFile(identity, private, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowed, client.Public(), 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"-identity", identity, "-allowed-client", allowed}, server, client
}

type startupWriter func([]byte) (int, error)

func (w startupWriter) Write(p []byte) (int, error) { return w(p) }

func TestListenFailureProducesNoReadiness(t *testing.T) {
	args, _, _ := provisionServer(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	var out bytes.Buffer
	args = append(args, "-listen", occupied.Addr().String())
	if err := run(context.Background(), args, &out); err == nil {
		t.Fatal("occupied listener accepted")
	}
	if out.Len() != 0 {
		t.Fatal("listen failure reported readiness")
	}
}

func TestStartupOutputFailureReleasesListener(t *testing.T) {
	args, _, _ := provisionServer(t)
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	outputErr := errors.New("test readiness output failure")
	called := false
	writer := startupWriter(func(p []byte) (int, error) {
		called = true
		if string(p) != "echo server listening\n" {
			t.Error("unexpected readiness output")
		}
		probe, err := net.Listen("tcp", address)
		if err == nil {
			probe.Close()
			t.Error("readiness emitted before listener ownership")
		}
		return 0, outputErr
	})
	args = append(args, "-listen", address)
	if err := run(context.Background(), args, writer); !errors.Is(err, outputErr) {
		t.Fatalf("output failure lost: %v", err)
	}
	if !called {
		t.Fatal("readiness writer not reached")
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("failed startup retained its listener")
	}
	rebound.Close()
}

func TestStartupCancellationJoinsAndReleasesListener(t *testing.T) {
	args, server, client := provisionServer(t)
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	reserved.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	writer := startupWriter(func(p []byte) (int, error) {
		if string(p) != "echo server listening\n" {
			return 0, errors.New("unexpected readiness output")
		}
		ready <- struct{}{}
		return len(p), nil
	})
	go func() { done <- run(ctx, append(args, "-listen", address), writer) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server command did not join")
		}
		rebound, err := net.Listen("tcp", address)
		if err != nil {
			t.Error("server command retained its listener")
		} else {
			rebound.Close()
		}
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("server command never became ready")
	}
	requestCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if err := echoapp.Exchange(requestCtx, address, client, server.Public()); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidOptionsProduceNoOutput(t *testing.T) {
	for _, args := range [][]string{nil, {"-identity", "private-path"}, {"-allowed-client", "pin-path"}, {"-unknown", "sensitive-input"}, {"extra"}, {"-identity", "missing-private", "-allowed-client", "missing-pin"}} {
		var out bytes.Buffer
		if err := run(context.Background(), args, &out); err == nil {
			t.Fatal("invalid input accepted")
		}
		if out.Len() != 0 {
			t.Fatal("failure produced listening output")
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
		t.Fatal("canceled server produced output")
	}
}
