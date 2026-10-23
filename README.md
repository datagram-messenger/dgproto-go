<div align="center">

# dgproto-go

**Go implementation of the DGProto v1 wire protocol and secure session runtime.**

[![CI](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://github.com/datagram-messenger/dgproto-go/blob/main/go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](https://github.com/datagram-messenger/dgproto-go/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/datagram-messenger/dgproto-go)](https://github.com/datagram-messenger/dgproto-go/releases/latest)

</div>

---

`dgproto-go` is a Go 1.25+ implementation of the **DGProto v1** wire protocol — a binary, session-oriented, cryptographically secured transport designed for low-latency bidirectional communication between native clients and Go backends.

This is a **protocol library**, not an application server. It handles everything from TCP framing to Noise handshakes, AEAD encryption, replay protection, and rekeying — so you can focus on your application logic.

> **Status:** The DGProto v1 specification is a draft. The protocol and Go module are versioned independently. Wire compatibility should be claimed only after stabilization.

## How it works

DGProto v1 is structured in five layers, each with a clear responsibility:

| Layer | Role | What it does |
|-------|------|--------------|
| **L4** | Application | Typed messages: chat, ping/pong, ack, close, rekey, error |
| **L3** | Session | Sequence numbers, epochs, rekeying, replay protection, keepalive |
| **L2** | Cryptography | Noise XX handshake, ChaCha20-Poly1305 AEAD, HKDF key schedule |
| **L1** | Framing | 40-byte fixed header, TLV encoding, optional padding |
| **L0** | Transport | TCP stream — body length derived from header, no length prefix |

## Install

```sh
go get github.com/datagram-messenger/dgproto-go
```

## Quick start

Generate a key, create a listener, and start accepting connections:

```go
key, err := dgproto.GenerateStaticKey()
if err != nil {
    log.Fatal(err)
}

listener, err := net.Listen("tcp", ":9000")
if err != nil {
    log.Fatal(err)
}

server, err := dgproto.NewServer(dgproto.ServerConfig{
    StaticKey: key,
    Handler: func(ctx context.Context, conn *dgproto.Connection, message any) error {
        log.Printf("received %T", message)
        return nil
    },
})
if err != nil {
    log.Fatal(err)
}

if err := server.Serve(listener); err != nil && !errors.Is(err, dgproto.ErrServerClosed) {
    log.Fatal(err)
}
```

See [Getting started](docs/guides/getting-started.md) for the full example with admission, authorization, and shutdown guidance.

## Key concepts

**Sending**

- `Send` — enqueues a message into the bounded outbound queue. Returns when the message is accepted, not when it is written or received.
- `SendAndWait` — waits for the local write to complete. Success means the frame was written to the socket, not acknowledged by the peer.
- `SendPadded` — sends with explicit zero padding. Padding policy is an application responsibility.

**Lifecycle**

- `Connection.Close()` — performs a graceful close handshake.
- `Server.Close()` — stops the listener and gracefully closes active connections.
- `Server.Abort()` — immediately terminates the server and all connections.

The first observed terminal cause is always retained. Later operations observe that result.

**Authorization**

Noise XX authenticates the peer's static key — it does not grant application permissions. Use `ServerConfig.AllowedClients` to restrict connections to a known key allowlist, and `ServerConfig.Admission` to apply custom authorization logic after authentication and before any messages are dispatched.

**Production timeouts**

`NewServer` uses a 10-second `HandshakeTimeout` when the configured value is zero;
zero does not disable the handshake deadline. `ReadTimeout`, `WriteTimeout`,
`IdleTimeout`, and `KeepaliveInterval` remain zero (disabled) by default. When
keepalive is enabled with a zero `KeepaliveTimeout`, its effective timeout is
twice `KeepaliveInterval`. For internet-facing deployments:

| Field | Suggested value | Purpose |
|-------|----------------|---------|
| `HandshakeTimeout` | 10 s | Bound unauthenticated TCP connections |
| `ReadTimeout` | ≥ keepalive window | Per-frame read deadline |
| `WriteTimeout` | 5–30 s | Per-frame write deadline |
| `IdleTimeout` | 5–15 min | Close inactive connections |
| `KeepaliveInterval` | 30–60 s | How often a Ping is sent |
| `KeepaliveTimeout` | 10–30 s | How long an unanswered Ping is tolerated |

## Security

DGProto v1 uses `Noise_XX_25519_ChaChaPoly_SHA256` for the handshake and ChaCha20-Poly1305 for all data frames. The MVP profile is fixed — no cipher negotiation, no 0-RTT, no resumption.

**Do not report vulnerabilities in public issues.** Use the [GitHub security advisory form](https://github.com/datagram-messenger/dgproto-go/security/advisories/new) instead. See [SECURITY.md](SECURITY.md) for the full policy.

## Documentation

| Document | Description |
|----------|-------------|
| [Getting started](docs/guides/getting-started.md) | Server setup, admission, lifecycle, and timeout guidance |
| [Architecture](docs/architecture/overview.md) | Implementation structure and concurrency model |
| [Protocol specification](docs/protocol/dgproto-v1.md) | Normative wire format and protocol requirements |
| [Test vectors](testdata/vectors/README.md) | Interoperability fixture scope and validation levels |
| [Contributing](.github/CONTRIBUTING.md) | Contribution workflow and requirements |

## License

Apache 2.0 — see [LICENSE](LICENSE).
