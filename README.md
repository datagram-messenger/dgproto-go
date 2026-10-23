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

Use a security-patched Go toolchain. Windows TCP clients require Go 1.25.10 or a later patched release to address the standard-library advisory [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971); the module's minimum language version is not a security-patch guarantee.

This is a **protocol library**, not an application server. It handles everything from TCP framing to Noise handshakes, AEAD encryption, replay protection, and rekeying — so you can focus on your application logic.

> **Status:** The DGProto v1 specification is a draft. The protocol and Go module are versioned independently. Wire compatibility should be claimed only after stabilization.

## How it works

DGProto v1 is structured in five layers, each with a clear responsibility:

| Layer | Role | What it does |
|-------|------|--------------|
| **L4** | Application | Typed messages: chat, ping/pong, ack, close, rekey, error |
| **L3** | Session | Sequence numbers, epochs, rekeying, replay protection, keepalive |
| **L2** | Cryptography | Noise XX handshake, ChaCha20-Poly1305 AEAD, Noise Split traffic keys |
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
- `SendContext` / `SendPaddedContext` — wait for bounded queue capacity, not network I/O.
- `SendPaddedAndWait` — waits for the local write with 0–255 random cleartext padding bytes authenticated with the exact header. Zero means no padding.
- Keep queued messages and reachable buffers immutable. Cancellation after enqueue does not retract work or permit immediate buffer reuse; no send promises peer acknowledgement.

**Pinned TCP client**

`DialContext(ctx, address, ClientConfig)` requires a client identity and a provisioned 32-byte `ServerStatic` pin. It returns a **stopped** connection plus `AdmissionInfo` (no traffic keys). Call `Start(runtimeCtx)` explicitly and always close the connection. Establishment uses the earlier of the caller deadline and `ConnectionConfig.HandshakeTimeout` (zero: 10 seconds for dial plus handshake); canceling the dial context after success does not end the runtime. Success is not server admission or application authorization. See the [runnable pinned exchange](client_example_test.go).

**Lifecycle**

- `Connection.Close()` — attempts a close-frame write then stops transport; no peer acknowledgement or outbound queue-drain guarantee.
- `Server.Close()` — stops networking and waits for server-owned work. Handlers/hooks must cooperate with cancellation.
- `Server.Abort()` — closes networking, including sockets in admission and runtime handoff, without waiting for handlers/hooks; it does not kill callback goroutines.
- Termination discards queued outbound/control/handler references and completes pending local-write waits with an error, including connections closed before `Start`. An in-progress writer or handler retains its own message until it exits; this is not delivery or a secure-memory-erasure guarantee.
- `ServerConfig.HandlerShutdownTimeout` rejects negative values. Zero is an unbounded handler wait; positive values may detach a canceled handler, not kill it. Admission and disconnect hooks must bound their own work.
- Direct `TCPTransport` calls honor cancellation while waiting for the read/write lane. Interrupted I/O may leave a partial frame; do not assume the stream remains reusable.
- Direct session callers must transmit the exact emitted `RekeyInit`, then call `MarkRekeySent` before retrying their message. Mutation of emitted ciphertext or padding is rejected against an independent snapshot.

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
| [0.6.0 hardening](docs/0.6.0-candidate.md) | Release scope, local validation, and remaining gates |

## License

Apache 2.0 — see [LICENSE](LICENSE).
