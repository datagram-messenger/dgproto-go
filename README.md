# dgproto

> Go implementation of **DGProto v1** — a binary,
> session-oriented, cryptographically secured application protocol for
> low-latency bidirectional communication between native clients and Go backends.

[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Features](#features)
4. [Wire Header](#wire-header-40-bytes)
5. [Message Types](#message-types)
6. [Quick Start](#quick-start)
7. [Connection API](#connection-api)
8. [Dependencies](#dependencies)
9. [Documentation](#documentation)
10. [Contributing](#contributing)

---

## Overview

DGProto v1 is a self-contained Go package that provides everything needed to
establish mutually-authenticated, encrypted, session-oriented connections over
TCP. It is designed for use in the Datagram Messenger system, where
Rust/Tauri desktop clients communicate with Go microservice backends.

The MVP profile fixes the transport to **TCP** and the cipher to
**ChaCha20-Poly1305**. Frames begin directly with the 40-byte `DGP1` header —
there is no separate length prefix.

---

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
| `0x07` | Reserved (post-MVP)               | —                |
| `0x08` | RekeyInit                         | sender direction |

---

## Quick Start

```go
import (
    dgproto "github.com/datagram-messenger/dgproto-go"
)

// Generate or load a long-term static key pair
staticKey, err := dgproto.GenerateStaticKey()
if err != nil {
    log.Fatal(err)
}

// Server — accept TCP connections and perform Noise XX
srv, err := dgproto.NewServer(dgproto.ServerConfig{
    StaticKey:   staticKey,
    CipherSuite: dgproto.CipherChaCha20Poly1305,
    Handler: func(ctx context.Context, conn *dgproto.Connection, msg any) error {
        if m, ok := msg.(*dgproto.EncryptedData); ok {
            return conn.Send(dgproto.EncryptedData{Payload: m.Payload}) // echo
        }
        return nil
    },
})
if err != nil {
    log.Fatal(err)
}

ln, _ := net.Listen("tcp", ":4242")
srv.Serve(ln)
```

For a step-by-step guide including client setup and connection configuration,
see [`docs/guides/getting-started.md`](docs/guides/getting-started.md).

---

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

## Dependencies

| Module | Purpose |
|---|---|
| `github.com/flynn/noise` | Noise Protocol Framework state machine |
| `golang.org/x/crypto` | ChaCha20-Poly1305 AEAD |

No application-layer or server-framework dependencies. The package is
intentionally self-contained so it can be imported by both server and client
implementations.

---

## Documentation

| Document | Description |
|---|---|
| [`docs/protocol/dgproto-v1.md`](docs/protocol/dgproto-v1.md) | Normative wire specification |
| [`docs/architecture/overview.md`](docs/architecture/overview.md) | Package layout and data-flow diagrams |
| [`docs/guides/getting-started.md`](docs/guides/getting-started.md) | Integration guide |

For a higher-level routing, authentication, and middleware layer see
[dgpserver](https://github.com/datagram-messenger/datagram-server/tree/main/pkg/dgpserver).

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow, test
requirements, wire-vector format, and code style guide.
