<div align="center">

# DGProto for Go

**The protocol repository for Datagram.**

A strict Go implementation of the DGProto v1 wire protocol and secure session runtime.

[![CI](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml/badge.svg)](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

[Quick start](#quick-start) · [Protocol](docs/protocol/dgproto-v1.md) · [Documentation](#documentation) · [Contributing](CONTRIBUTING.md)

</div>

---

## Overview

This repository contains **Datagram's protocol implementation**, not the
Datagram application server. The `dgproto` Go package implements the strict MVP
profile of DGProto v1: binary framing, typed messages, a three-flight Noise XX
handshake, authenticated sessions, replay protection, rekeying, and a TCP
connection runtime.

The MVP has a deliberately fixed profile:

- TCP stream transport;
- `Noise_XX_25519_ChaChaPoly_SHA256`;
- ChaCha20-Poly1305 data-frame encryption;
- a 40-byte little-endian `DGP1` header with no outer length prefix.

The [DGProto v1 specification](docs/protocol/dgproto-v1.md) is the source of
truth for wire behavior. The Go API is documented on
[pkg.go.dev](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go).

> [!IMPORTANT]
> DGProto is security-sensitive infrastructure. Persist static private keys
> securely, authenticate peer static keys, and review the specification before
> changing wire-visible behavior.

## Project status and versioning

**DGProto v1 / specification 1.0.0** identifies the protocol and its draft wire
specification. **Go module v0.1.0** identifies the planned library release;
these version numbers are independent.

The specification remains **Draft — Implementation Track**. Until the wire
profile is declared stable and interoperability-tested, neither specification
`1.0.0` nor the `v1` name guarantees wire compatibility between revisions.
Likewise, Go module `v0.1.0` is pre-v1: consumers should expect source API
changes and pin an exact reviewed tag or commit.

The current implementation includes server-side connection acceptance and an
explicit initiator handshake API. It is a protocol library, not a complete
messenger backend: application routing, account authentication, persistence,
and service configuration belong in the related
[Datagram Server](https://github.com/datagram-messenger/server) repository.

## Architecture

DGProto v1 separates concerns into four layers stacked over TCP:

```
┌──────────────────────────────────────────┐
│  L3  Application Messages                │
│      (EncryptedData, Ping, Ack, Rekey…)  │
├──────────────────────────────────────────┤
│  L2  Session Layer                       │
│      (state machine, sequence numbers,   │
│       replay window, atomic rekeying)    │
├──────────────────────────────────────────┤
│  L1  Cryptographic Layer                 │
│      (Noise XX handshake, ChaCha20-      │
│       Poly1305 AEAD, HKDF key schedule)  │
├──────────────────────────────────────────┤
│  L0  Framing Layer                       │
│      (40-byte LE wire header, TLV,       │
│       anti-fingerprinting padding)       │
└──────────────────────────────────────────┘
         TCP stream transport
```

See [`docs/architecture/overview.md`](docs/architecture/overview.md) for the
full package layout and data-flow diagrams.

---

## Features

- **Noise XX mutual authentication** — `Noise_XX_25519_ChaChaPoly_SHA256`,
  1.5-RTT; both peers authenticate with long-term X25519 static keys.
- **Session IDs from channel binding** —
  `SHA-256("DGPv1 SessionID" || noise_channel_binding)[0:16]`, derived
  independently by both peers after the third Noise flight.
- **Replay protection** — per-direction 64-bit sliding window
  (IPsec/WireGuard model), epoch-aware.
- **Atomic rekeying** — HMAC-SHA256 key ratchet with constant-time
  confirmation; sender triggers at 2³² frames or 10 minutes, receiver enforces
  strict epoch ordering.
- **Anti-fingerprinting padding** — random cleartext padding, length bucketed
  (256 / 512 / 1024 / 1500 bytes), authenticated inside the AEAD AAD without
  being encrypted.
- **Little-endian wire format** — matches native encoding on x86-64 and
  aarch64; safe for zero-copy `zerocopy` struct reinterpretation in Rust
  clients.
- **TLV envelope** — 1-byte type, 2-byte LE length, 4-byte aligned value;
  used for variable-length L3 payloads.
- **Connection runtime** — concurrent read / write / maintenance goroutines
  with idle timeout, keepalive ping/pong, and graceful shutdown.

---

## Wire Header (40 bytes)

| Field          | Offset | Size | Description                                           |
|----------------|--------|------|-------------------------------------------------------|
| Magic          | 0      | 4    | `44 47 50 31` (ASCII `DGP1`)                          |
| Version        | 4      | 1    | `0x01`                                                |
| Flags          | 5      | 1    | Bit 1: padding present. Other bits reserved.          |
| Msg Type       | 6      | 1    | See [message type registry](#message-types) below.    |
| Reserved       | 7      | 1    | Zero on send, preserved on receive.                   |
| Session ID     | 8      | 16   | 128-bit session identifier (zero during handshake).   |
| Sequence       | 24     | 8    | Per-direction monotonic counter; AEAD nonce material. |
| Payload Length | 32     | 4    | Ciphertext length, excluding tag and padding.         |
| Pad Length     | 36     | 1    | Trailing random padding length (0–255 bytes).         |
| Reserved       | 37     | 3    | Zero on send, preserved on receive.                   |

Followed by: `Payload` · `AEAD Tag (16 bytes, data frames only)` · `Padding`.

---

## Message Types

| Value  | Name                              | Direction        |
|--------|-----------------------------------|------------------|
| `0x01` | HandshakeInit                     | client → server  |
| `0x02` | HandshakeResponse / HandshakeFinish | both           |
| `0x03` | EncryptedData                     | both             |
| `0x04` | Ping / Pong                       | both             |
| `0x05` | SessionClose                      | both             |
| `0x06` | Ack                               | both             |
| `0x07` | Reserved (post-MVP resumption ticket) | —              |
| `0x08` | RekeyInit                         | sender direction |
| `0x09` | Error                             | both             |

---

## Prerequisites

- [Go 1.25 or newer](https://go.dev/doc/install)
- Git, when cloning the repository or contributing
- A C toolchain for race-detector runs (`go test -race`)

## Quick start

Add the module to an existing Go project:

```sh
go get github.com/datagram-messenger/dgproto-go
```

The following minimal program starts a DGProto server. It accepts any client
whose Noise handshake is valid; production applications should configure
`AllowedClients` or use `Admission` to enforce an explicit identity policy.

```go
package main

import (
    "context"
    "errors"
    "log"
    "net"

    dgproto "github.com/datagram-messenger/dgproto-go"
)

func main() {
    staticKey, err := dgproto.GenerateStaticKey()
    if err != nil {
        log.Fatal(err)
    }
    // Persist staticKey securely and reuse it across restarts in production.

    srv, err := dgproto.NewServer(dgproto.ServerConfig{
        StaticKey: staticKey,
        Handler: func(_ context.Context, conn *dgproto.Connection, msg any) error {
            if data, ok := msg.(*dgproto.EncryptedData); ok {
                return conn.Send(dgproto.EncryptedData{
                    StreamID:       data.StreamID,
                    AppMessageType: data.AppMessageType,
                    Fields:         data.Fields,
                })
            }
            return nil
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    listener, err := net.Listen("tcp", "127.0.0.1:4242")
    if err != nil {
        log.Fatal(err)
    }
    defer listener.Close()

    if err := srv.Serve(listener); err != nil && !errors.Is(err, dgproto.ErrServerClosed) {
        log.Fatal(err)
    }
}
```

Run the sample from a separate module (the repository root is a library, not a
`main` package). Save the program as `main.go` in an empty directory, then run:

```sh
go mod init example.com/dgproto-sample
go get github.com/datagram-messenger/dgproto-go@v0.1.0
go run .
```

Before `v0.1.0` is published, replace the version with a reviewed commit hash.

The package intentionally exposes the client/initiator handshake as an explicit
three-flight state machine. See the
[getting-started guide](docs/guides/getting-started.md) for client setup,
identity handling, connection configuration, and backpressure behavior.

## Connection API

| Method | Blocking | Description |
|---|---|---|
| `Send(msg)` | no | Enqueue; returns `ErrOutboundQueueFull` if full |
| `TrySend(msg)` | no | Explicit nonblocking form of `Send` |
| `SendContext(ctx, msg)` | yes | Block until slot free or ctx cancelled |
| `SendAndWait(ctx, msg)` | yes | Block until transport write completes |
| `SendPadded(msg, pad)` | no | Control per-frame anti-fingerprint padding |
| `Close()` | yes | Send `SessionClose`, then stop the runtime |
| `Done()` | — | Channel closed when all loops exit |
| `Err()` | — | Terminal cause after `Done()` is closed |

---

## Scope and limitations

The strict MVP supports TCP, Noise XX, ChaCha20-Poly1305, encrypted application
messages, Ping/Pong, acknowledgements, session close, replay protection,
directional rekeying, padding, bounded queues, connection limits, and graceful
shutdown.

It does **not** implement or negotiate QUIC, transport obfuscation, Noise IK,
resumption tickets, 0-RTT, or alternative cipher suites. Message type `0x07` and
flags reserved for post-MVP features are rejected by the session API. Padding
reduces length-based fingerprinting but does not hide the fixed `DGP1` magic or
provide transport obfuscation.

The server permits any successfully authenticated Noise peer when
`AllowedClients` is empty and no rejecting `Admission` handler is configured.
Applications are responsible for defining and persisting their peer-identity
policy.

## Development

Run these commands from the repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run TestJSONWireVectors ./...
go test -run MVP ./...
```

The race detector requires CGO and a supported C toolchain. See the
[contributing guide](CONTRIBUTING.md) for focused test commands, fuzzing,
benchmarks, wire-vector maintenance, and protocol-change requirements.

## Repository structure

```text
docs/
  architecture/   Package layout and data flow
  guides/         Integration guidance
  protocol/       Normative DGProto v1 specification
testdata/          Cross-language wire vectors
*.go               Framing, crypto, handshake, session, transport, and runtime
```

## Dependencies

| Module | Purpose |
|---|---|
| `github.com/flynn/noise` | Noise Protocol Framework state machine |
| `golang.org/x/crypto` | ChaCha20-Poly1305 and cryptographic primitives |

No application framework is included. Protocol behavior remains concentrated in
this module so server and client implementations can share the same wire rules.

## Documentation

| Document | Description |
|---|---|
| [`docs/protocol/dgproto-v1.md`](docs/protocol/dgproto-v1.md) | Normative wire specification |
| [`docs/architecture/overview.md`](docs/architecture/overview.md) | Package layout and data-flow diagrams |
| [`docs/guides/getting-started.md`](docs/guides/getting-started.md) | Integration guide |
| [`SECURITY.md`](SECURITY.md) | Private vulnerability reporting policy |

## Related repositories

- [Datagram Server](https://github.com/datagram-messenger/server) — Datagram's Go backend and service runtime.
- [`dgpserver`](https://github.com/datagram-messenger/server/tree/main/pkg/dgpserver) — higher-level routing, authentication, middleware, and lifecycle package built on DGProto.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting changes. Wire-visible
changes require specification updates, compatibility review, and updated test
vectors. Security-sensitive changes should remain small and explicitly tested.

## License

Licensed under the [Apache License 2.0](LICENSE).
