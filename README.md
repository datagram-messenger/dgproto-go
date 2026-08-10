# dgpv1

Go implementation of the **Datagram Protocol v1 (DGPv1)** — a binary, session-oriented, cryptographically secured application protocol for low-latency bidirectional communication between native clients and Go backends.

## Overview

DGPv1 separates concerns into four layers stacked over TCP:

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

The MVP profile fixes the transport to TCP and the cipher to **ChaCha20-Poly1305**. Frames begin directly with the 40-byte DGP1 header — there is no separate length prefix.

## Features

- **Noise XX mutual authentication** — `Noise_XX_25519_ChaChaPoly_SHA256`, 1.5-RTT, both peers authenticate with long-term X25519 static keys.
- **Session IDs from channel binding** — `SHA-256("DGPv1 SessionID" || noise_channel_binding)[0:16]`, derived independently by both peers after the third Noise flight.
- **Replay protection** — per-direction 64-bit sliding window (IPsec/WireGuard model), epoch-aware.
- **Atomic rekeying** — HMAC-SHA256 key ratchet with constant-time confirmation; sender triggers at 2³² frames or 10 minutes, receiver enforces strict epoch ordering.
- **Anti-fingerprinting padding** — random cleartext padding, length bucketed (256/512/1024/1500 bytes), authenticated inside the AEAD AAD without being encrypted.
- **Little-endian wire format** — matches native encoding on x86-64 and aarch64; safe for zero-copy `zerocopy` struct reinterpretation in Rust clients.
- **TLV envelope** — 1-byte type, 2-byte LE length, 4-byte aligned value; used for variable-length L3 payloads.

## Wire Header (40 bytes)

| Field          | Offset | Size | Description                                              |
|----------------|--------|------|----------------------------------------------------------|
| Magic          | 0      | 4    | `44 47 50 31` (ASCII `DGP1`)                             |
| Version        | 4      | 1    | `0x01`                                                   |
| Flags          | 5      | 1    | Bit 1: padding present. Other bits reserved.             |
| Msg Type       | 6      | 1    | See message type registry below.                         |
| Reserved       | 7      | 1    | Zero on send, preserved on receive.                      |
| Session ID     | 8      | 16   | 128-bit session identifier (zero during handshake).      |
| Sequence       | 24     | 8    | Per-direction monotonic counter; AEAD nonce material.    |
| Payload Length | 32     | 4    | Ciphertext length, excluding tag and padding.            |
| Pad Length     | 36     | 1    | Trailing random padding length (0–255 bytes).            |
| Reserved       | 37     | 3    | Zero on send, preserved on receive.                      |

Followed by: `Payload` · `AEAD Tag (16 bytes, data frames only)` · `Padding`.

## Message Types

| Value  | Name                | Direction        |
|--------|---------------------|------------------|
| `0x01` | HandshakeInit       | client → server  |
| `0x02` | HandshakeResponse / HandshakeFinish | both |
| `0x03` | EncryptedData       | both             |
| `0x04` | Ping / Pong         | both             |
| `0x05` | SessionClose        | both             |
| `0x06` | Ack                 | both             |
| `0x07` | Reserved (post-MVP) | —                |
| `0x08` | RekeyInit           | sender direction |

## Dependencies

```
github.com/flynn/noise    — Noise protocol framework
golang.org/x/crypto       — ChaCha20-Poly1305
```

No application-layer or server-framework dependencies. The package is intentionally self-contained so it can be imported by both server and client implementations.

## Usage

```go
import "github.com/datagram-messenger/protocol"

// Generate or load a long-term static key pair
staticKey, err := dgpv1.GenerateStaticKey()

// Server side — accept a TCP connection and perform Noise XX
srv, err := dgpv1.NewServer(dgpv1.ServerConfig{
    StaticKey:   staticKey,
    CipherSuite: dgpv1.CipherChaCha20Poly1305,
    Handler: dgpv1.MessageHandlerFunc(func(ctx context.Context, conn *dgpv1.Connection, msg *dgpv1.EncryptedData) {
        _ = conn.Send(ctx, msg.Payload) // echo
    }),
})

ln, _ := net.Listen("tcp", ":4242")
srv.Serve(ln)
```

For a higher-level routing and authentication layer see [dgpserver](https://github.com/datagram-messenger/datagram-server/tree/main/pkg/dgpserver).

## Specification

The full wire specification is in [`docs/protocol/dgp-v1.md`](https://github.com/datagram-messenger/datagram-server/blob/main/docs/protocol/dgp-v1.md).
