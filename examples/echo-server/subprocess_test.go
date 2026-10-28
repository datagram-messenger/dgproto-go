package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// Bounded capture never prints child output (which could accidentally contain
// fixture contents). Ready is signaled only by the exact command readiness line.
type processOutput struct {
	mu    sync.Mutex
	data  []byte
	ready chan struct{}
	once  sync.Once
}

func (o *processOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	if remaining := 4096 - len(o.data); remaining > 0 {
		o.data = append(o.data, p[:min(len(p), remaining)]...)
	}
	if o.ready != nil && bytes.Equal(o.data, []byte("echo server listening\n")) {
		o.once.Do(func() { close(o.ready) })
	}
	return n, nil
}

func (o *processOutput) equals(want string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.data) == want
}

func smokeEnvironment() []string {
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, "DGPROTO_KEYLOG") && !strings.EqualFold(name, "GOWORK") {
			env = append(env, entry)
		}
	}
	return append(env, "GOWORK=off")
}

func TestSeparateProcessProvisionedEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and launches two example binaries")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Derive public trust files locally before either peer is started; no TOFU.
	provision := func(name string) (string, string) {
		private := make([]byte, 32)
		if _, err := rand.Read(private); err != nil {
			t.Fatal("synthetic key generation failed")
		}
		defer clear(private)
		key, err := dgproto.LoadStaticKey(private)
		if err != nil {
			t.Fatal("synthetic identity invalid")
		}
		identity, public := filepath.Join(dir, name+".key"), filepath.Join(dir, name+".pub")
		if os.WriteFile(identity, private, 0600) != nil || os.WriteFile(public, key.Public(), 0600) != nil {
			t.Fatal("temporary provisioning failed")
		}
		return identity, public
	}
	serverKey, serverPin := provision("server")
	clientKey, clientPublic := provision("client")
	_, wrongPin := provision("untrusted")
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	serverBinary, clientBinary := filepath.Join(dir, "echo-server"+suffix), filepath.Join(dir, "echo-client"+suffix)
	buildCtx, stopBuild := context.WithTimeout(context.Background(), 60*time.Second)
	defer stopBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", dir+string(os.PathSeparator), "./examples/echo-server", "./examples/echo-client")
	build.Dir, build.Env, build.WaitDelay = root, smokeEnvironment(), 2*time.Second
	build.Stdout, build.Stderr = io.Discard, io.Discard
	if err := build.Run(); err != nil {
		t.Fatal("example binary build failed")
	}
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	// The command has no inherited-listener interface. There is a small local
	// reserve/rebind window; a collision fails readiness rather than retrying trust.
	serverCtx, cancelServer := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelServer()
	stdout, stderr := &processOutput{ready: make(chan struct{})}, &processOutput{}
	server := exec.CommandContext(serverCtx, serverBinary, "-listen", address, "-identity", serverKey, "-allowed-client", clientPublic)
	server.Env, server.Stdout, server.Stderr, server.WaitDelay = smokeEnvironment(), stdout, stderr, 2*time.Second
	if err := server.Start(); err != nil {
		t.Fatal("server subprocess start failed")
	}
	done := make(chan struct{})
	var serverErr error
	go func() { serverErr = server.Wait(); close(done) }()
	t.Cleanup(func() {
		// Kill only our directly launched executable, then join its sole Wait.
		cancelServer()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("server subprocess teardown did not join")
		}
	})
	select {
	case <-stdout.ready:
	case <-done:
		t.Fatal("server exited before readiness")
	case <-time.After(5 * time.Second):
		t.Fatal("server readiness timed out")
	}
	runClient := func(pin string, success bool) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, failure := &processOutput{}, &processOutput{}
		client := exec.CommandContext(ctx, clientBinary, "-address", address, "-identity", clientKey, "-server-pin", pin)
		client.Env, client.Stdout, client.Stderr, client.WaitDelay = smokeEnvironment(), out, failure, 2*time.Second
		err := client.Run()
		if ctx.Err() != nil {
			t.Fatal("client subprocess exceeded deadline")
		}
		if success {
			if err != nil || !out.equals("authenticated application echo received\n") || !failure.equals("") {
				t.Fatal("client subprocess did not report exactly one validated echo")
			}
		} else {
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 || !out.equals("") || !failure.equals("echo client failed (check inputs, trust and server availability)\n") {
				t.Fatal("wrong-pin subprocess did not fail safely")
			}
		}
	}
	runClient(serverPin, true)
	runClient(wrongPin, false)
	runClient(serverPin, true) // Failed trust must not break subsequent admission.
	if !stdout.equals("echo server listening\n") || !stderr.equals("") {
		t.Fatal("unexpected server subprocess output")
	}
	select {
	case <-done:
		t.Fatal("server exited before requested teardown")
	default:
	}
	if runtime.GOOS == "windows" {
		t.Log("Windows teardown uses process kill; console Ctrl+C is not exercised")
		cancelServer()
	} else {
		if err := server.Process.Signal(os.Interrupt); err != nil {
			t.Fatal("server interrupt failed")
		}
	}
	select {
	case <-done:
		if runtime.GOOS != "windows" && serverErr != nil {
			t.Fatal("interrupted server did not exit successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server stop did not join")
	}
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("stopped subprocess retained listener")
	}
	rebound.Close()
}
