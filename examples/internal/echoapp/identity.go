// Package echoapp shares application-specific code between the runnable echo
// commands. It is not a reusable client facade or part of the library API.
package echoapp

import (
	"bytes"
	"errors"
	"io"
	"os"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// ErrIdentityInput means a required provisioned key file is missing or invalid.
// Errors intentionally do not contain paths or key material.
var ErrIdentityInput = errors.New("echo: invalid identity input")

// LoadIdentity reads exactly 32 raw private-key bytes and uses the library's
// existing loader. Provisioning, filesystem permissions and rotation are external.
func LoadIdentity(path string) (dgproto.StaticKey, error) {
	private, err := readKey(path)
	if err != nil {
		return dgproto.StaticKey{}, err
	}
	defer clear(private) // Best effort only; not a guarantee of memory erasure.
	return dgproto.LoadStaticKey(private)
}

// LoadPublic reads a required nonzero 32-byte raw public pin or allowlist entry.
// It never discovers, replaces, generates or persists a trusted identity.
func LoadPublic(path string) ([]byte, error) { return readKey(path) }

func readKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrIdentityInput
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(data) != 32 || bytes.Equal(data, make([]byte, 32)) {
		clear(data)
		return nil, ErrIdentityInput
	}
	return data, nil
}
