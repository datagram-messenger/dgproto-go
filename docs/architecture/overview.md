# Architecture

This document describes the Go implementation. The [protocol specification](../protocol/dgproto-v1.md) is normative for wire behavior.

## File map

- `header.go`, `frame.go`, `tlv.go`, `messages.go`: headers, frame validation, TLVs, and typed messages.
- `crypto.go`: ChaCha20-Poly1305, nonce formation, and authenticated encryption.
- `handshake.go`: Noise XX handshake state and key derivation.
- `session.go`, `rekey.go`, `replay.go`: epochs, directional keys, rekey transitions, and replay windows.
- `transport.go`, `tcp.go`: frame transport and TCP framing (no outer length prefix; body length is derived from the fixed header).
- `connection.go`: reader, writer, handler, queues, and lifecycle.
- `server.go`: listener, limits, admission, and shutdown.

## Send and rekey flow

Application calls enqueue typed messages into a bounded outbound queue. The single writer path serializes the message, applies explicit zero padding, selects the current send epoch and increasing sequence, authenticates the header as associated data, encrypts with ChaCha20-Poly1305, and writes the frame. `SendAndWait` observes this local writer result only.

Rekey is serialized with sending. The writer emits the rekey transition and advances directional epoch/key state in protocol order so ordinary sends cannot race that transition. This is not application acknowledgement or correlation.

## Receive, replay, and previous epoch

The reader validates framing and headers before decryption. It selects current or permitted previous receive-epoch state, applies the 2048-entry replay window, authenticates and decrypts, parses the typed payload, and commits replay state under the successful-processing rules. Previous-epoch state exists narrowly for in-flight transition traffic and is retired by the rekey state machine; arbitrary old epochs are rejected.

## Concurrency and lifecycle

Each connection has dedicated read and write loops and a handler worker. Exported methods are safe for concurrent use, while writes are serialized. Received application messages enter a bounded handler queue and handlers run serially. No pooling, batching, parallel-handler, lock-free, or other unimplemented optimization is implied.

Termination is coordinated once and retains the first observed cause. It cancels connection context, unblocks queued work, closes transport state, and makes later operations observe the terminal result. Handler errors and panics terminate the connection. Admission occurs after authentication and before loops start; server limits bound handshakes and established connections independently.
