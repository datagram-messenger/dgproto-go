# Architecture

This document describes the Go implementation. The [protocol specification](../protocol/dgproto-v1.md) is normative for wire behavior. The [package layout plan](package-layout.md) records the incremental reorganization checkpoint and next scoped block.

## File map

- `header.go`, `frame.go`: public headers and frame validation.
- `tlv.go`: concrete public TLV, exact copies/empty shapes, current error sentinels and descriptor allocation.
- `internal/tlv/codec.go`: bounded arithmetic and two-pass primitive byte codec; colocated `codec_test.go` holds golden/alignment and hostile-input algorithm cases. Root `tlv_api_test.go` checks consumer identity/error/ownership/allocation; `tlv_decode_limits_test.go`, `tlv_encode_regression_test.go` and `tlv_decode_benchmark_test.go` retain public integration/resource coverage.
- `messages.go`: shared message sizes/errors and application/control message families with shared TLV/text validation.
- `messages_handshake.go`: real root NoisePattern and three-flight wrapper definitions/codecs; `messages_handshake_test.go` checks external concrete identity, errors and final-flight ownership/rejection.
- `crypto.go`: ChaCha20-Poly1305, nonce formation, and authenticated encryption.
- `handshake.go`: Noise XX handshake state and key derivation.
- `client.go`: required-pin TCP dial and bounded establishment; returns a stopped caller-owned runtime with identity metadata and no traffic secrets.
- `session.go`: central secrets/state/construction/close and shared typed codec dispatch.
- `session_send.go`: send serialization, sequence allocation and pending send-rekey transaction/commit.
- `session_receive.go`: candidate authentication and atomic replay/receive-epoch/rekey/grace validation and commit.
- `rekey.go`: rekey policy and confirmation.
- `replay.go`: public replay types, unchanged value storage, tokens, root sentinels and forwarding methods.
- `internal/replay/window.go`: replay validation, commit and bitmap shift implementation; `window_test.go` contains the relocated algorithm scenarios. `replay_api_test.go` covers the root facade.
- `transport.go`, `tcp.go`: frame transport and TCP framing (no outer length prefix; body length is derived from the fixed header).
- `connection.go`: central connection/config/error declarations, constructor, and shared operation contexts.
- `connection_receive.go`: receive/control routing, serial handler dispatch, bounded shutdown wait and panic containment.
- `connection_lifecycle.go`: start/close/abort coordination, Done ownership, terminal cause and shutdown.
- `connection_send.go`: send/queue API, enqueue admission, single writer/rekey ordering, completion and pending disposal.
- `connection_keepalive.go`: maintenance loop, idle/keepalive timers, timeout default, ping expectation/ACK and activity helpers.
- `server.go`: central server/config/error definitions, listener, limits, admission/disconnect, handoff and shutdown.
- `server_handshake.go`: responder establishment, authenticated allowlist and handshake-frame validation shared with the client; root symbols and wire behavior unchanged.
- `internal/acceptpolicy/errors.go`: stateless accept-error classification; the server retains backoff and lifecycle ownership.

## Package direction and development

The root public package owns concrete wire/message/replay types, error sentinels and session/runtime state. Its internal dependencies are `internal/tlv`, `internal/acceptpolicy` and `internal/replay`, each importing only the standard library and never root. The TLV leaf owns byte parsing/writing and bounded arithmetic; root supplies concrete descriptors and one copy per nonempty decoded value without intermediate conversion allocation. Allocation-free failure details are wrapped around the root's current sentinel variables. Full validation precedes second-pass reading and public output allocation; internal Read/Write preconditions do not relax public hostile-input rejection. The replay leaf owns validation/commit/bitmap algorithms over primitive caller-owned storage; root retains the exact ReplayWindow/ReplayToken representation and sentinel variables. No pointer-backed window, internal type alias, or runtime interface is introduced. Cohesive root files are source organization, not alias facades or separate packages: the handshake wrapper family stays root-owned to preserve public identity, method sets, error wrapping and buffer ownership. Session authentication/replay/rekey commits and lifecycle locks remain together. Follow [test placement and contribution guidance](../../.github/CONTRIBUTING.md#where-to-add-code-and-tests) when adding code; this layout does not certify production readiness.

## Send and rekey flow

Application calls enqueue typed messages into a bounded outbound queue without copying their reachable buffers. The single writer serializes the message, selects the current send epoch and increasing sequence, encrypts the payload with ChaCha20-Poly1305, and authenticates the exact 40-byte header plus random cleartext trailing padding as associated data. Requested padding is 0–255 bytes; zero means no padding. `SendAndWait` and `SendPaddedAndWait` observe only the complete local writer result, including required rekey. Contextual admission waits for capacity; cancellation after enqueue does not retract work or imply serialization has finished.

Rekey is serialized with sending. The writer emits the rekey transition and advances directional epoch/key state in protocol order so ordinary sends cannot race that transition. This is not application acknowledgement or correlation.

## Receive, replay, and previous epoch

The reader validates framing before decryption and authenticates the exact received header (including preserved unknown flags/reserved bytes) plus cleartext padding. It selects current or permitted previous receive-epoch state and checks the 2048-entry replay window. Failed authentication does not advance replay state; an authenticated malformed ordinary message consumes its sequence. Rekey validation and epoch/key/replay/grace changes commit atomically. Previous-epoch state exists narrowly for in-flight transition traffic and is retired by the rekey state machine; arbitrary old epochs are rejected.

## Concurrency and lifecycle

Each connection has dedicated read and write loops and a handler worker. Exported methods are safe for concurrent use, while writes are serialized. Received application messages enter a bounded handler queue and handlers run serially. No pooling, batching, parallel-handler, lock-free, or other unimplemented optimization is implied.

Termination is coordinated once and retains the first observed cause. It cancels connection context, unblocks queued work, closes transport state, and makes later operations observe the terminal result. Handler errors and panics terminate the connection. Admission occurs after authentication and before loops start; server limits bound handshakes and established connections independently.
