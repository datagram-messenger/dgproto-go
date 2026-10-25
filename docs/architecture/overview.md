# Architecture

This document describes the Go implementation. The [protocol specification](../protocol/dgproto-v1.md) is normative for wire behavior. The [package layout plan](package-layout.md) records the incremental reorganization checkpoint and next scoped block.

## File map

- `header.go`, `frame.go`: real public Header/Flags/MessageType, current Magic/error variables, semantic header/frame validation and owned output.
- `internal/wire/header.go`: complete lossless fixed-header little-endian byte codec and normative shape constants, using transient primitive values without allocation. `header_test.go` covers golden/every-octet/precondition cases; root `header_api_test.go` and `header_wire_test.go` cover public identity/errors/ownership/allocation and exact received AAD octets.
- `tlv.go`: concrete public TLV, exact copies/empty shapes, current error sentinels and descriptor allocation.
- `internal/tlv/codec.go`: bounded arithmetic and two-pass primitive byte codec; colocated `codec_test.go` holds golden/alignment and hostile-input algorithm cases. Root `tlv_api_test.go` checks consumer identity/error/ownership/allocation; `tlv_decode_limits_test.go`, `tlv_encode_regression_test.go` and `tlv_decode_benchmark_test.go` retain public integration/resource coverage.
- `messages.go`: real public message definitions, shared sizes/current error variables, validation and owned allocation; application/TLV/text codecs remain here.
- `internal/wire/control.go`: complete primitive PingPong, Ack, RekeyInit and post-MVP ticket byte layouts, reusing the existing wire leaf rather than adding a tiny package. `control_test.go` covers literal bytes, all Ack counts, suffix/zero overwrite, borrowed internal ticket views, buffer preconditions and zero primitive allocations. Root `messages_api_test.go` checks concrete identity, method sets, current/nil sentinel chains, error priority, unchanged-on-error/nil-receiver behavior, ownership, boundaries and allocation budgets; vectors/conformance/message fuzz remain root.
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
- `integrationtests/tcp_test.go`: public-import-only `net.Pipe` integration checks for oversized-header rejection before body I/O, EOF/malformed frames, and read-context cancellation. Run `go test -count=1 ./integrationtests` from the repository root; `go test -count=1 ./...` includes this test-only package. Shared-helper TCP tests and private client/runtime seams remain adjacent to root implementation; runnable `client_example_test.go` remains root for godoc/README.
- `connection.go`: central connection/config/error declarations, constructor, and shared operation contexts.
- `connection_receive.go`: receive/control routing, serial handler dispatch, bounded shutdown wait and panic containment.
- `connection_lifecycle.go`: start/close/abort coordination, Done ownership, terminal cause and shutdown.
- `connection_send.go`: send/queue API, enqueue admission, single writer/rekey ordering, completion and pending disposal.
- `connection_keepalive.go`: maintenance loop, idle/keepalive timers, timeout default, ping expectation/ACK and activity helpers.
- `server.go`: central server/config/error definitions, default/config validation, listener accept/backoff and shutdown snapshots/join.
- `server_runtime.go`: complete per-socket worker, admission/disconnect containment, independent slot leases, worker reservation and atomic socket-to-runtime handoff. All methods still use the same root Server state and shutdown mutex; `server_runtime_test.go` checks both serialized handoff/shutdown snapshots alongside existing hardening stress tests.
- `server_handshake.go`: responder establishment, authenticated allowlist and handshake-frame validation shared with the client; root symbols and wire behavior unchanged.
- `internal/acceptpolicy/errors.go`: stateless accept-error classification; the server retains backoff and lifecycle ownership.

## Package direction and development

The root public package owns concrete wire/message/replay types, error sentinels and session/runtime state. Its internal dependencies are `internal/wire`, `internal/tlv`, `internal/acceptpolicy` and `internal/replay`, each importing only the standard library and never root. The wire leaf owns whole fixed-header and control-payload byte packing/unpacking; root retains policy, current mutable variables and allocation. Fixed-header lossless stack-value conversion keeps AAD/copy/allocation contracts; its historical CPU cost and the current control-boundary benchmarks are recorded in the package-layout checkpoints. Control readers use validated buffers and owned scalar output, except for the explicitly borrowed transient ticket view copied once by root. The TLV leaf owns byte parsing/writing and bounded arithmetic; root supplies concrete descriptors and one copy per nonempty decoded value without intermediate conversion allocation. Allocation-free failure details are wrapped around the root's current sentinel variables. Full validation precedes second-pass reading and public output allocation; internal Read/Write preconditions do not relax public hostile-input rejection. The replay leaf owns validation/commit/bitmap algorithms over primitive caller-owned storage; root retains the exact ReplayWindow/ReplayToken representation and sentinel variables. No pointer-backed window, internal type alias, or runtime interface is introduced. Cohesive root files are source organization, not alias facades or separate packages: the handshake wrapper family stays root-owned to preserve public identity, method sets, error wrapping and buffer ownership. Session authentication/replay/rekey commits and lifecycle locks remain together. Follow [test placement and contribution guidance](../../.github/CONTRIBUTING.md#where-to-add-code-and-tests) when adding code; this layout does not certify production readiness.

## Send and rekey flow

Application calls enqueue typed messages into a bounded outbound queue without copying their reachable buffers. The single writer serializes the message, selects the current send epoch and increasing sequence, encrypts the payload with ChaCha20-Poly1305, and authenticates the exact 40-byte header plus random cleartext trailing padding as associated data. Requested padding is 0–255 bytes; zero means no padding. `SendAndWait` and `SendPaddedAndWait` observe only the complete local writer result, including required rekey. Contextual admission waits for capacity; cancellation after enqueue does not retract work or imply serialization has finished.

Rekey is serialized with sending. The writer emits the rekey transition and advances directional epoch/key state in protocol order so ordinary sends cannot race that transition. This is not application acknowledgement or correlation.

## Receive, replay, and previous epoch

The reader validates framing before decryption and authenticates the exact received header (including preserved unknown flags/reserved bytes) plus cleartext padding. It selects current or permitted previous receive-epoch state and checks the 2048-entry replay window. Failed authentication does not advance replay state; an authenticated malformed ordinary message consumes its sequence. Rekey validation and epoch/key/replay/grace changes commit atomically. Previous-epoch state exists narrowly for in-flight transition traffic and is retired by the rekey state machine; arbitrary old epochs are rejected.

## Concurrency and lifecycle

Each connection has dedicated read and write loops and a handler worker. Exported methods are safe for concurrent use, while writes are serialized. Received application messages enter a bounded handler queue and handlers run serially. No pooling, batching, parallel-handler, lock-free, or other unimplemented optimization is implied.

Termination is coordinated once and retains the first observed cause. It cancels connection context, unblocks queued work, closes transport state, and makes later operations observe the terminal result. Handler errors and panics terminate the connection. Admission occurs after authentication and before loops start; server limits bound handshakes and established connections independently.
