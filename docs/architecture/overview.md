# Architecture

This document describes the Go implementation. The [protocol specification](../protocol/dgproto-v1.md) is normative for wire behavior. The [package layout plan](package-layout.md) records the incremental reorganization checkpoint and next scoped block.

## File map

- `header.go`, `frame.go`, `tlv.go`, `messages.go`: headers, frame validation, TLVs, and typed messages.
- `crypto.go`: ChaCha20-Poly1305, nonce formation, and authenticated encryption.
- `handshake.go`: Noise XX handshake state and key derivation.
- `client.go`: required-pin TCP dial and bounded establishment; returns a stopped caller-owned runtime with identity metadata and no traffic secrets.
- `session.go`, `rekey.go`, `replay.go`: epochs, directional keys, rekey transitions, and replay windows.
- `transport.go`, `tcp.go`: frame transport and TCP framing (no outer length prefix; body length is derived from the fixed header).
- `connection.go`: central connection/config/error declarations, constructor, reader, handler, and lifecycle.
- `connection_send.go`: send/queue API, enqueue admission, single writer/rekey ordering, completion and pending disposal.
- `connection_keepalive.go`: maintenance loop, idle/keepalive timers, timeout default, ping expectation/ACK and activity helpers.
- `server.go`: listener, limits, admission, and shutdown.
- `internal/acceptpolicy/errors.go`: stateless accept-error classification; the server retains backoff and lifecycle ownership.

## Send and rekey flow

Application calls enqueue typed messages into a bounded outbound queue without copying their reachable buffers. The single writer serializes the message, selects the current send epoch and increasing sequence, encrypts the payload with ChaCha20-Poly1305, and authenticates the exact 40-byte header plus random cleartext trailing padding as associated data. Requested padding is 0–255 bytes; zero means no padding. `SendAndWait` and `SendPaddedAndWait` observe only the complete local writer result, including required rekey. Contextual admission waits for capacity; cancellation after enqueue does not retract work or imply serialization has finished.

Rekey is serialized with sending. The writer emits the rekey transition and advances directional epoch/key state in protocol order so ordinary sends cannot race that transition. This is not application acknowledgement or correlation.

## Receive, replay, and previous epoch

The reader validates framing before decryption and authenticates the exact received header (including preserved unknown flags/reserved bytes) plus cleartext padding. It selects current or permitted previous receive-epoch state and checks the 2048-entry replay window. Failed authentication does not advance replay state; an authenticated malformed ordinary message consumes its sequence. Rekey validation and epoch/key/replay/grace changes commit atomically. Previous-epoch state exists narrowly for in-flight transition traffic and is retired by the rekey state machine; arbitrary old epochs are rejected.

## Concurrency and lifecycle

Each connection has dedicated read and write loops and a handler worker. Exported methods are safe for concurrent use, while writes are serialized. Received application messages enter a bounded handler queue and handlers run serially. No pooling, batching, parallel-handler, lock-free, or other unimplemented optimization is implied.

Termination is coordinated once and retains the first observed cause. It cancels connection context, unblocks queued work, closes transport state, and makes later operations observe the terminal result. Handler errors and panics terminate the connection. Admission occurs after authentication and before loops start; server limits bound handshakes and established connections independently.
