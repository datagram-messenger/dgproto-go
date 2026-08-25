# Contributing to DGPv1

Thank you for your interest in contributing! This document covers the workflow,
coding standards, and test requirements for the
`github.com/datagram-messenger/protocol` package.

---

## Table of Contents

1. [Development Setup](#1-development-setup)
2. [Running Tests](#2-running-tests)
3. [Benchmarks](#3-benchmarks)
4. [Fuzz Testing](#4-fuzz-testing)
5. [Wire Vectors](#5-wire-vectors)
6. [Code Style](#6-code-style)
7. [Submitting Changes](#7-submitting-changes)
8. [Specification Changes](#8-specification-changes)

---

## 1. Development Setup

```bash
git clone https://github.com/datagram-messenger/protocol
cd protocol
go mod download
```

**Requirements:** Go 1.25+. No other tooling is required for core development.

---

## 2. Running Tests

```bash
# full test suite (race detector on)
go test -race ./...

# verbose output for a specific file
go test -race -v -run TestHandshake ./...

# MVP constraint compliance tests
go test -race -v -run TestMVP ./...
```

All tests must pass with `-race` before a PR is merged.

### Test File Map

| File | Covers |
|---|---|
| `frame_test.go` | L0 wire header encode/decode |
| `tlv_test.go` | TLV envelope |
| `header_test.go` | header field validation |
| `crypto_test.go` | Noise XX handshake, AEAD |
| `handshake_test.go` | three-flight handshake state machine |
| `session_test.go` | session send/receive, sequence numbers |
| `replay_test.go` | sliding-window anti-replay |
| `rekey_test.go` | HMAC-SHA256 key ratchet |
| `messages_test.go` | message serialization |
| `message_extended_types_test.go` | extended message types |
| `connection_test.go` | Connection runtime (timeouts, keepalives) |
| `server_test.go` | Server accept loop |
| `tcp_test.go` | TCPTransport framing |
| `send_api_test.go` | Send/SendContext/SendAndWait APIs |
| `send_shutdown_property_test.go` | shutdown-under-send properties |
| `session_transport_edge_cases_test.go` | edge cases in session+transport |
| `mvp_constraints_test.go` | MVP profile enforcement |
| `wire_vectors_test.go` | JSON wire-vector corpus |
| `benchmark_test.go` | performance benchmarks |
| `parser_fuzz_test.go` | fuzz entry points |

---

## 3. Benchmarks

```bash
go test -bench=. -benchmem -benchtime=5s ./...
```

When adding a performance-sensitive code path, include a benchmark so
regressions are visible in CI.

---

## 4. Fuzz Testing

```bash
# run the frame parser fuzzer for 60 seconds
go test -fuzz=FuzzFrameUnmarshalBinary -fuzztime=60s

# run indefinitely (use Ctrl-C to stop)
go test -fuzz=FuzzFrameUnmarshalBinary
```

Interesting corpus entries found by the fuzzer should be committed under
`testdata/fuzz/`.

---

## 5. Wire Vectors

JSON test vectors live in `testdata/vectors/`. Each file is a strict object:

```json
{
  "schema": "dgpv1-wire-v1",
  "vectors": [
    {
      "name": "unique-name",
      "kind": "header",
      "wire_hex": "44475031...",
      "valid": true
    }
  ]
}
```

Rules:
- `name` must be unique within the file.
- `wire_hex` must be lowercase with no spaces or separators.
- Valid vectors must round-trip: decode → re-encode → exact same bytes.
- Invalid vectors must fail with the named `error` sentinel.
- The loader rejects unknown fields, trailing JSON, and duplicate names.

When fixing a parser bug or adding a new message type, add at least one valid
and one invalid vector covering the new behaviour.

---

## 6. Code Style

- **`gofmt`** — all code must be `gofmt`-formatted. Run `gofmt -l .` before committing.
- **`go vet`** — must produce zero warnings.
- **Exported symbols** — every exported type, function, and constant must have
  a godoc comment. Package-level docs live in `doc.go`.
- **Error wrapping** — use `fmt.Errorf("dgpv1: ...: %w", err)` with a
  `dgpv1:` prefix and a sentinel exported error where callers may need to
  `errors.Is`/`errors.As`.
- **No panics in library code** — except for programming errors (nil
  dependency injection). Document any `panic` call.
- **Little-endian** — use `binary.LittleEndian` for all multi-byte integer
  encoding; never `binary.BigEndian` or `binary.NativeEndian`.
- **Constant-time comparisons** — use `subtle.ConstantTimeCompare` wherever
  security-sensitive byte strings are compared (key confirmation, session IDs).

---

## 7. Submitting Changes

1. **Fork** the repository and create a feature branch off `main`.
2. Keep commits small and focused; write a meaningful commit message.
3. Ensure `go test -race ./...` passes locally.
4. Open a Pull Request with:
   - A description of *what* changed and *why*.
   - A reference to any related issue.
   - Confirmation that new tests / vectors were added where appropriate.
5. A maintainer will review and may request changes. Address feedback in
   new commits (do not force-push during review).

---

## 8. Specification Changes

The wire format is normative. **Any change that affects on-wire bytes,
message types, or cryptographic derivations MUST be accompanied by a matching
update to [`docs/protocol/dgp-v1.md`](docs/protocol/dgp-v1.md).**

Backward-incompatible changes require a new version section or a new
specification document. Do not silently alter the existing normative text
without updating the version or status field at the top of the spec.
