# DGPv1 — Architecture Overview

## Package Layout

```
github.com/datagram-messenger/protocol
├── doc.go                  — package-level godoc
├── frame.go                — L1 wire header (40 bytes), Frame type
├── tlv.go                  — TLV envelope encode/decode
├── crypto.go               — Noise XX handshake, AEAD helpers, HKDF key schedule
├── handshake.go            — three-flight handshake state machine
├── session.go              — L2/L3 session: sequence numbers, replay window
├── rekey.go                — atomic HMAC-SHA256 key ratchet
├── replay.go               — 64-bit sliding-window anti-replay
├── messages.go             — L3 message types (EncryptedData, Ping, Ack, …)
├── header.go               — header encode/decode helpers
├── transport.go            — TCPTransport interface
├── tcp.go                  — TCP stream framing (ReadFrame / WriteFrame)
├── connection.go           — Connection runtime (read/write/maintenance loops)
├── server.go               — Server: Accept loop, per-connection lifecycle
├── testdata/vectors/       — JSON wire-format test vectors
└── docs/
    ├── protocol/dgp-v1.md  — normative wire specification
    ├── architecture/       — this file and related design notes
    └── guides/             — integration and usage guides
```

## Layered Model

```
┌──────────────────────────────────────────┐
│  L3  Application Messages                │
│      (EncryptedData, Ping, Ack, Rekey…)  │  messages.go
├──────────────────────────────────────────┤
│  L2  Session Layer                       │
│      (state machine, sequence numbers,   │  session.go
│       replay window, atomic rekeying)    │  replay.go / rekey.go
├──────────────────────────────────────────┤
│  L1  Cryptographic Layer                 │
│      (Noise XX handshake, ChaCha20-      │  crypto.go / handshake.go
│       Poly1305 AEAD, HKDF key schedule)  │
├──────────────────────────────────────────┤
│  L0  Framing Layer                       │
│      (40-byte LE wire header, TLV,       │  frame.go / tlv.go / header.go
│       anti-fingerprinting padding)       │
└──────────────────────────────────────────┘
         TCP stream transport               tcp.go / transport.go
```

## Key Design Decisions

### Little-Endian Wire Format
All multi-byte integers are little-endian to match native encoding on x86-64
and aarch64. This enables zero-copy `zerocopy` struct reinterpretation in the
Rust client without byte-swapping overhead.

### Noise XX — No Custom Crypto
The handshake uses the standard `Noise_XX_25519_ChaChaPoly_SHA256` pattern
from the Noise Protocol Framework. DGPv1 does **not** invent its own key
exchange or authentication scheme. The `github.com/flynn/noise` library
provides the Noise state machine.

### Session ID from Channel Binding
After the third Noise flight both peers independently derive:
```
SessionID = SHA-256("DGPv1 SessionID" || channel_binding)[0:16]
```
This ties the session identifier cryptographically to the completed handshake
transcript, preventing session-fixation attacks.

### Replay Window
Per-direction 64-bit sliding window (IPsec/WireGuard model). Implemented in
`replay.go`; integrated into `session.go` on every `Receive` call.

### Atomic Rekeying
Directional HMAC-SHA256 key ratchet. The sender triggers at 2³² frames or
10 minutes. The receiver verifies a constant-time `KeyConfirm` before
installing the new key. See `rekey.go` and §4.4.1 of the spec.

### Connection Runtime
`Connection` owns three goroutines:
- **readLoop** — reads frames from `TCPTransport`, dispatches to `Session.Receive`.
- **writeLoop** — drains the outbound channel, calls `Session.Send`, writes frames.
- **maintenanceLoop** — idle timeout, keepalive ping/pong, graceful shutdown.

A fourth **handlerLoop** goroutine is started when a `MessageHandler` is
configured. All loops share a single `context.WithCancelCause` so any fatal
error propagates cleanly.

## Data Flow — Sending a Message

```
caller → Connection.Send(msg)
           └─ enqueue to outbound chan
writeLoop ← dequeue
           └─ Session.Send(msg, padLen)
                 └─ serialize → encrypt (ChaCha20-Poly1305) → Frame
           └─ TCPTransport.WriteFrame(ctx, frame)
                 └─ write 40-byte header + ciphertext + tag + padding to TCP
```

## Data Flow — Receiving a Message

```
TCPTransport.ReadFrame(ctx)
    └─ read exactly 40 bytes (header) → parse PayloadLength + PadLength
    └─ read PayloadLength + 16 (tag) + PadLength bytes
Session.Receive(frame)
    └─ replay-window check
    └─ AEAD decrypt (ChaCha20-Poly1305)
    └─ deserialize → typed message (EncryptedData / PingPong / …)
Connection.readLoop → dispatch to handlerQueue or handle internally
```

## Dependencies

| Module | Purpose |
|---|---|
| `github.com/flynn/noise` | Noise Protocol Framework state machine |
| `golang.org/x/crypto` | ChaCha20-Poly1305 AEAD |
| `golang.org/x/sys` | indirect (via x/crypto) |

No application-layer or server-framework dependencies. The package is
intentionally self-contained so it can be imported by both server and client
implementations without pulling in unrelated transitive dependencies.
