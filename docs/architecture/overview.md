# Architecture

This document describes the Go implementation. The [protocol specification](../protocol/dgproto-v1.md) is normative for wire behavior. The [package layout](package-layout.md) records the earlier reorganization checkpoint; its unresolved ErrorMessage TLV discrepancy paragraph is superseded by the raw Error implementation described below and the [migration guide](../guides/error-message-migration.md). The [archived chronology](package-layout-history.md) preserves earlier observations/proposals; its recommendations and inventories are not current guidance.

## File map

- `header.go`, `frame.go`: real public Header/Flags/MessageType, current Magic/error variables, semantic header/frame validation and owned output. Complete public frame layout/validation/ownership tests live in `integrationtests/api/frame_test.go`; private AAD, vectors/conformance and fuzz tests remain root.
- `internal/wire/header.go`: complete lossless fixed-header little-endian byte codec and normative shape constants, using transient primitive values without allocation. `header_test.go` covers golden/every-octet/precondition cases; `integrationtests/api/header_api_test.go` and `header_wire_test.go` cover public identity/errors/ownership/allocation and exact received AAD octets.
- `tlv.go`: concrete public TLV, exact copies/empty shapes, current error sentinels and descriptor allocation.
- `internal/tlv/codec.go`: bounded arithmetic and two-pass primitive byte codec; colocated `codec_test.go` holds golden/alignment and hostile-input algorithm cases. `integrationtests/api/tlv_api_test.go` checks consumer identity/error/ownership/allocation; `tlv_decode_limits_test.go`, `tlv_encode_regression_test.go` and `tlv_decode_benchmark_test.go` retain public integration/resource coverage.
- `messages.go`: shared sizes/current error variables and real public primitive control wrappers, validation and owned allocation.
- `messages_tlv.go`: real root EncryptedData/SessionClose, complete typed-TLV uniqueness and UTF-8 text validation/codec closure. `integrationtests/api/messages_tlv_api_test.go` covers external consumer identity/interfaces, copy/empty shapes, validation priority, unchanged destinations and current/nil sentinels/receivers. No new internal error/descriptor seam.
- `messages_error.go`: root ErrorMessage implements normative `code:u16 LE || context:raw UTF-8`, not TLV, with owned output, unchanged-on-error decode and `MaxErrorContextSize`. `integrationtests/api/messages_error_api_test.go` owns its public contract checks. Nonempty Error is wire-incompatible with the old TLV codec, without negotiation; SessionClose, session cryptography and lifecycle remain unchanged. See the [migration guide](../guides/error-message-migration.md), including Rust's preserved public u8 restriction.
- `internal/wire/control.go`: complete primitive PingPong, Ack, RekeyInit and post-MVP ticket byte layouts, reusing the existing wire leaf rather than adding a tiny package. `control_test.go` covers literal bytes, all Ack counts, suffix/zero overwrite, borrowed internal ticket views, buffer preconditions and zero primitive allocations. `integrationtests/api/messages_api_test.go` checks concrete identity, method sets, current/nil sentinel chains, error priority, unchanged-on-error/nil-receiver behavior, ownership, boundaries and allocation budgets; vectors/conformance/message fuzz remain root.
- `messages_handshake.go`: real root NoisePattern and three-flight wrapper definitions/codecs; `integrationtests/api/messages_handshake_test.go` checks external concrete identity, errors and final-flight ownership/rejection. `integrationtests/api/messages_test.go` holds the complete cross-family public golden/round-trip/ownership/truncation/text/wrapper rejection suite; it imports only root and stdlib, with no helper seam.
- `crypto.go`: ChaCha20-Poly1305, nonce formation, and authenticated encryption.
- `static_key.go`: real root StaticKey identity, generation/loading, validation, owned public/Noise key copies and the private loading reader; `static_key_test.go` covers concrete storage identity, copy boundaries and current/nil sentinels.
- `handshake.go`: complete Noise XX flight state machine, peer authentication and transcript-derived session material; static identity operations live separately without moving runtime state into an internal package.
- `client.go`: required-pin TCP dial and bounded establishment; returns a stopped caller-owned runtime with identity metadata and no traffic secrets.
- `session.go`: central secrets/state/construction/close and shared typed codec dispatch.
- `session_send.go`: send serialization, sequence allocation and pending send-rekey transaction/commit.
- `session_receive.go`: candidate authentication and atomic replay/receive-epoch/rekey/grace validation and commit.
- `rekey.go`: rekey policy and confirmation.
- `replay.go`: public replay types, unchanged value storage, tokens, root sentinels and forwarding methods.
- `internal/replay/window.go`: replay validation, commit and bitmap shift implementation; `window_test.go` contains the relocated algorithm scenarios. `integrationtests/api/replay_api_test.go` covers the root facade.
- `transport.go`, `tcp.go`: frame transport and TCP framing (no outer length prefix; body length is derived from the fixed header).
- `integrationtests/tcp_test.go`: public-import-only `net.Pipe` integration checks for oversized-header rejection before body I/O, EOF/malformed frames, and read-context cancellation. Run `go test -count=1 ./integrationtests` from the repository root; `go test -count=1 ./...` includes this test-only package. Shared-helper TCP tests and private client/runtime seams remain adjacent to root implementation; runnable `client_example_test.go` remains root for godoc/README.
- `connection.go`: central connection/config/error declarations, constructor, and shared operation contexts.
- `connection_receive.go`: receive/control routing, serial handler dispatch, bounded shutdown wait and panic containment.
- `connection_lifecycle.go`: start/close/abort coordination, Done ownership, terminal cause and shutdown.
- `connection_send.go`: send/queue API, enqueue admission, single writer/rekey ordering, completion and pending disposal.
- `connection_keepalive.go`: maintenance loop, idle/keepalive timers, timeout default, ping expectation/ACK and activity helpers.
- `server_config.go`: public ServerConfig and the complete NewServer startup transaction: validation/default ordering followed by initial map/channel allocation. `server_config_test.go` owns constructor regressions, normalized/explicit snapshots and error priority; shared key/config helpers remain in root server tests.
- `server.go`: central Server/error/admission definitions, listener accept/backoff and shutdown snapshots/join. Startup configuration is separate source organization in the same root package, not a new runtime owner; Server locks, slots and handoff remain unchanged.
- `server_runtime.go`: complete per-socket worker, admission/disconnect containment, independent slot leases, worker reservation and atomic socket-to-runtime handoff. All methods still use the same root Server state and shutdown mutex; `server_runtime_test.go` checks both serialized handoff/shutdown snapshots alongside existing hardening stress tests.
- `server_handshake.go`: responder establishment, authenticated allowlist and handshake-frame validation shared with the client; root symbols and wire behavior unchanged.
- `internal/acceptpolicy/errors.go`: stateless accept-error classification; the server retains backoff and lifecycle ownership.
- `keylog.go`, `keylog_disabled.go`: tagged sink forwarding and unchanged default no-ops. Only the tagged facade imports `internal/keylog/writer.go`, which owns the complete process-wide environment/open-once/file/leaf-lock/record-formatting responsibility. Colocated tagged tests cover exact synthetic record bytes, append/single-open, no retry after absent/failed opening, and concurrent non-interleaved initial pairs/epoch records. Root `keylog_enabled_test.go` keeps private Session/rekey commit integration in a fresh subprocess, with no production reset/close API. Session commit points and lock ownership are unchanged.

## Frame/message test-boundary checkpoint

The complete frame and cross-family typed-message suites now live in
`integrationtests/api/frame_test.go` and `integrationtests/api/messages_test.go`.
Both are external consumers (`package dgproto_test`) with explicit public imports;
neither depends on root-private helpers. The existing assertions, literal bytes,
copy/error priority, receive-reserved fields, text/wrapper limits and truncation
cases are retained. Root implementation, dependencies, tags and public API are
unchanged; this is a test visibility boundary, not a parser or crypto rewrite.

A fresh TCP audit kept its stream owner whole: each direction owns a serialized
lane, queued waits do not alter deadlines, and cancellation watchers join before
the deadline is cleared and the lane released. Root framing retains Header/Frame
validation and owned allocation, and root error adaptation retains current
sentinels and context priority shared with client establishment. Extracting only
lane/watch helpers would be a micro-utility; extracting the entire stream engine
would need protocol/error adaptation plus additional concurrency/performance
justification. Neither is warranted merely to increase package count.

Root network tests retain injected dialers, private started/key/abort state and
shared `testTransportFrame` dependencies. TLV maximum-size tests use private
alignment arithmetic; crypto tests use private nonce/AEAD/AAD/entropy seams.
No helpers were duplicated, assertions weakened or private APIs exported for a
move. Future test work should separate whole helper closures by responsibility,
not treat necessary public root files or atomic runtime owners as layout defects.

## Package direction and development

The root public package owns concrete wire/message/replay types, error sentinels and session/runtime state. Its default internal dependencies are `internal/wire`, `internal/tlv`, `internal/acceptpolicy` and `internal/replay`; only `dgproto_keylog` builds also depend on `internal/keylog`. Each leaf imports only the standard library and never root. The wire leaf owns whole fixed-header and control-payload byte packing/unpacking; root retains policy, current mutable variables and allocation. Fixed-header lossless stack-value conversion keeps AAD/copy/allocation contracts; its historical CPU cost and the current control-boundary benchmarks are recorded in the package-layout checkpoints. Control readers use validated buffers and owned scalar output, except for the explicitly borrowed transient ticket view copied once by root. The TLV leaf owns byte parsing/writing and bounded arithmetic; root supplies concrete descriptors and one copy per nonempty decoded value without intermediate conversion allocation. Allocation-free failure details are wrapped around the root's current sentinel variables. Full validation precedes second-pass reading and public output allocation; internal Read/Write preconditions do not relax public hostile-input rejection. The replay leaf owns validation/commit/bitmap algorithms over primitive caller-owned storage; root retains the exact ReplayWindow/ReplayToken representation and sentinel variables. No pointer-backed window, internal type alias, or runtime interface is introduced. Cohesive root files are source organization, not alias facades or separate packages: the handshake wrapper family stays root-owned to preserve public identity, method sets, error wrapping and buffer ownership. Session authentication/replay/rekey commits and lifecycle locks remain together. Follow [test placement and contribution guidance](../../.github/CONTRIBUTING.md#where-to-add-code-and-tests) when adding code; this layout does not certify production readiness.

## Send and rekey flow

Application calls enqueue typed messages into a bounded outbound queue without copying their reachable buffers. The single writer serializes the message, selects the current send epoch and increasing sequence, encrypts the payload with ChaCha20-Poly1305, and authenticates the exact 40-byte header plus random cleartext trailing padding as associated data. Requested padding is 0–255 bytes; zero means no padding. `SendAndWait` and `SendPaddedAndWait` observe only the complete local writer result, including required rekey. Contextual admission waits for capacity; cancellation after enqueue does not retract work or imply serialization has finished.

Rekey is serialized with sending. The writer emits the rekey transition and advances directional epoch/key state in protocol order so ordinary sends cannot race that transition. This is not application acknowledgement or correlation.

## Receive, replay, and previous epoch

The reader validates framing before decryption and authenticates the exact received header (including preserved unknown flags/reserved bytes) plus cleartext padding. It selects current or permitted previous receive-epoch state and checks the 2048-entry replay window. Failed authentication does not advance replay state; an authenticated malformed ordinary message consumes its sequence. Rekey validation and epoch/key/replay/grace changes commit atomically. Previous-epoch state exists narrowly for in-flight transition traffic and is retired by the rekey state machine; arbitrary old epochs are rejected.

## Concurrency and lifecycle

Each connection has dedicated read and write loops and a handler worker. Exported methods are safe for concurrent use, while writes are serialized. Received application messages enter a bounded handler queue and handlers run serially. No pooling, batching, parallel-handler, lock-free, or other unimplemented optimization is implied.

Termination is coordinated once and retains the first observed cause. It cancels connection context, unblocks queued work, closes transport state, and makes later operations observe the terminal result. Handler errors and panics terminate the connection. Admission occurs after authentication and before loops start; server limits bound handshakes and established connections independently.
