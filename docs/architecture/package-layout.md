# Incremental package layout

This is an architecture plan, not a release checklist or a compatibility certification. Read the [runtime overview](overview.md), [repository guide](../../AGENTS.md), [public package overview](../../doc.go), and [protocol specification](../protocol/dgproto-v1.md) together.

The authoritative resume point is **Current checkpoint: server worker ownership** below. Older checkpoints and next-step recommendations are historical; their package inventories do not describe the current tree.

## Current checkpoint: server worker ownership

Continued from clean `refactor/internal-packages` at `53777dc`. Implementation and coupled regression tests are committed as `5979989` (`refactor(server): group admission and runtime ownership`). No push, tag or release is part of this batch.

The audit found `server.go` the largest remaining production unit (453 lines), followed by `messages.go` (355) and `handshake.go` (310). Large root tests are white-box or shared-helper suites, not independent packages to move solely for file counts. Existing `wire`, `tlv`, `replay` and `acceptpolicy` leaves remain stdlib-only and acyclic. A new message-validation leaf would require another ownership/error boundary and performance evidence; small nonce/random helpers do not justify packages. Session receive authentication/replay/rekey and Connection lifecycle remain complete atomic transactions. Public concrete definitions stay in the mature Go convention of a root library package, not aliases to a relocated internal runtime.

Selected a bounded navigation improvement: nine complete unchanged functions move into [server_runtime.go](../../server_runtime.go): `handleConn`, `callAdmission`, `callDisconnect`, `tryAcquire`, `release`, `registerHandshake`, `unregisterHandshake`, `registerConn`, `unregisterConn`. This groups the whole worker/slot/admission/handoff responsibility rather than moving `handleConn` alone. `server.go` retains actual exported definitions, defaults/config validation, accept/backoff and Close/Abort/shutdown snapshot/join. Both files operate on the same Server mutex, maps, channels and wait group; reservation-before-Wait and socket-to-runtime handoff remain atomic under that mutex. No signature, field, sentinel, default, callback ordering, wire or cryptographic algorithm changes.

[server_runtime_test.go](../../server_runtime_test.go) adds deterministic coverage for both serialized handoff/shutdown outcomes, exclusive runtime versus socket snapshots, rejected post-shutdown registration and repeated notification/snapshot behavior. Existing worker-reservation, admission ownership, callback, slot-release and concurrent handoff hardening tests remain intact; assertions were not rewritten to make relocation easier.

### Observed server validation

Go 1.26.5 windows/amd64, CGO_ENABLED=1, CC=`zig cc`; module minimum remains Go 1.25.0. Fresh uncached default baseline passed. Focused baseline and post-move `go test -run "Test.*(Server|Admission|Disconnect|Handoff|Handshake|Slot|Abort|Shutdown|Close)" -count=1 ./...` passed. Post-move uncached `go test -count=1 ./...`, `go test -tags dgproto_keylog -count=1 ./...`, ordinary/tagged vet and ordinary/tagged build passed. `go test -run TestJSONWireVectors -count=1 ./...` passed. `gofmt -l .` was empty; only changed Go files were gofmt-written. Complete staged diff and cached whitespace checks passed before the implementation commit.

A temporary Go parser/source-slice checker compared all 249 root production declarations, including comments and both keylog variants, against the pre-move HEAD as a sorted multiset with only CRLF normalization: exact parity passed. This includes concrete type definitions, signatures, function bodies, constants and sentinels; the new regression test is separate. The checker is outside the module, not a shipped utility.

Fresh `go test -race -count=1 ./...` failed before test execution (exit 1): Zig's Windows external linker cannot resolve `WaitOnAddress`, `WakeByAddressSingle`, `WakeByAddressAll`. No race pass is claimed; supported Linux race and security-patched Go 1.25.x/platform CI remain external gates. Benchmarks and parser fuzz campaigns were not run because this is a declaration-identical root runtime move, not an algorithm/parser extraction. No dependency/module edits, fixture regeneration, vulnerability scan, independent interoperability or deployment certification.

This finishes the substantive bounded server batch. Defer a bounded message-validation leaf until its full error/ownership seam and comparative benchmarks are justified; do not fragment the completed runtime/session transactions or chase file-count reductions. The historical ErrorMessage specification discrepancy remains unchanged.

## Historical checkpoint: public network integration layer

Continued from clean, pushed `77ed860`; the checked test reorganization is committed as `1882905`. The test-only `integrationtests` package imports the public library through an explicit `dgproto` alias and the standard library only. No production implementation, export, dependency, fixture, timeout, ordering or rekey behavior changes.

Three self-contained declarations move from root `tcp_test.go` to [integrationtests/tcp_test.go](../../integrationtests/tcp_test.go): `TestTCPTransportRejectsOversizedHeaderBeforeBody`, `TestTCPTransportEOFAndMalformedFrame` (both existing subtests), and `TestTCPTransportContextCancellation`. These exercise the real public transport over `net.Pipe`, including rejection without reading a body and actual canceled I/O. Their bodies, names, assertions and constants are preserved apart from public qualification and replacing private `minFrameSize` with the exactly equivalent public `HeaderSize`. There are no helpers to duplicate or root-test dependencies to export. Remaining TCP helpers and their dependent tests stay root because hardening/session tests also use `testTransportFrame`.

The inspected `client_integration_test.go` is white-box despite its filename: it checks `started`, private key storage, injected `dialContext`, close counters and root helpers. `client_failure_test.go`/`client_test.go` likewise depend on private dial/handshake/config seams. `tcp_concurrent_echo_test.go` uses private `Connection.abort` in worker cleanup; replacing it with graceful `Close` or context cancellation would change cleanup/terminal semantics, so it remains root. Existing external `dgproto_test` files are public facade contracts or the executable example, not self-contained network suites to arbitrarily relocate. `client_example_test.go` stays root, with unchanged godoc output and README quick-start links. Internal algorithm tests stay adjacent to their implementations; root white-box, vector/conformance/fuzz and build-tag-specific keylog coverage stays intact. No obsolete editor paths are recreated and the ErrorMessage specification discrepancy remains untouched.

### Observed integration validation

Go 1.26.5 windows/amd64, CGO_ENABLED=1; module minimum remains Go 1.25.0. Exact baseline and moved selectors `^TestTCPTransport(RejectsOversizedHeaderBeforeBody|EOFAndMalformedFrame|ContextCancellation)$` passed uncached, including both EOF/malformed subtests. Baseline and final `go test -count=1 ./...`, default vet/build, `go test -tags dgproto_keylog -count=1 ./...`, tagged vet/build and empty `gofmt -l .` passed. A declaration-content checker confirmed all TCP functions/helpers against `77ed860` with only the two specified normalization rules and no duplicate/dropped declarations. `go list` showed the new test-only package, its public/std-library imports, and root `client_example_test.go` still visible as an external test. Compilation and execution in a separate directory directly verify the public import boundary.

Known unsupported Windows Zig race linking was not retried. Supported Linux race, security-patched minimum-Go and other-platform CI remain external gates; no CI success, independent interoperability or deployment guarantee is claimed. This closes the session's safe tests/docs organization batch; further migration should preserve complete helper closures and white-box assertions rather than rewrite behavior merely to increase moved-file count. Bounded-message abstraction work remains deferred, not a prerequisite or speculative follow-on in this batch.

## Historical checkpoint: fixed-header primitive codec

Continued from clean `refactor/internal-packages` at `25c2af9`; implementation and coupled tests are committed as `178ae8a`.

```text
dgproto (public concrete API, validation, copies, runtime ownership)
  -> internal/wire         -> encoding/binary
  -> internal/tlv          -> encoding/binary, fmt
  -> internal/replay       -> errors
  -> internal/acceptpolicy -> errors, net, syscall
```

`internal/wire/header.go` owns the complete 40-byte fixed-header little-endian pack/unpack and normative header/version/tag/frame-size constants. Its transient primitive Header includes the caller-supplied magic octets and retains every reserved byte and unknown flag/type. This is a whole codec, not an endian-helper bucket. Root `header.go` remains the real concrete Header/Flags/MessageType API with identical fields, methods, constant types/values, mutable Magic and writable error variables. Root retains all semantic validation and exact priority/diagnostics, checks length/current Magic/version before primitive decode, and assigns the public destination only after receive validation. Primitive functions require a full header buffer and return no borrowed storage; root conversions are stack values, not aliases, embedded storage, unsafe casts, interfaces or callbacks.

`frame.go`, TCP framing, crypto/AAD, session locks and runtime production files are untouched. Sender reserved flags/bytes remain strictly rejected, while receivers retain exact authenticated octets. No outer length prefix, 40-byte header/65535-byte total-frame contract, handshake tag exception, zero/default fields, nil-receiver/error behavior or ownership changes are introduced. Public constants now reference the leaf constants without changing their types or values. No dependency, module, fixture or specification changes occur; the historical ErrorMessage discrepancy stays deferred.

### Observed fixed-header validation and resource evidence

Go 1.26.5 windows/amd64, CGO_ENABLED=1, CC=`zig cc`; minimum Go remains 1.25.0. Baseline and final focused header/frame/vector/conformance/handshake/session selectors passed. Final uncached ordinary and `dgproto_keylog` full tests, ordinary/tagged vet, build, empty `gofmt -l .`, and unstaged/staged diff checks passed. `internal/wire/header_test.go` covers literal golden bytes, every offset at all 256 octet values, zero overwrites, suffix preservation, short-buffer preconditions and zero primitive allocations. External-package `header_api_test.go` covers concrete identity/field shape, marshaler method compatibility, current Magic, replacement/nil sentinels with exact text/priority, nil receiver panic boundary, copies and allocation budgets. `header_wire_test.go` checks every received offset/value against exact AAD re-encoding and unchanged-on-error storage; existing crypto reserved-byte tamper, padding AAD, vectors, conformance and frame ownership tests remain integration gates.

An isolated local-replace consumer ran the public API regressions, tests and vet, then compared baseline/current Header and Frame across 30,301 hostile/truncated/every-octet wire cases and 157,456 header cases spanning all message types, padding endpoints, uint32/frame length boundaries, sequence boundaries, reserved fields and current Magic. Values, encoded bytes/capacities/nil shape, exact error text and sentinel classification agreed. Its uniquely created temporary module was removed. This is differential Go implementation evidence, not independent interoperability.

Matched public benchmarks used `go test -run "^$" -bench "BenchmarkHeaderBoundary" -benchmem -benchtime=1s -count=3`: encode baseline 71.78–74.12 ns/op, final 79.85–80.80; decode baseline 26.15–27.95 ns/op, final 41.82–41.95. The small transient representation has a measurable CPU cost, explicitly accepted for a coherent safe boundary rather than hidden as a speedup. Allocation/storage budgets are unchanged: encode 1 allocation/48 B (returned len/cap 40), decode 0 allocations/0 B; both primitive functions inline and buffers do not escape on this toolchain. Timing samples are not statistical performance certification or service SLO evidence.

Real two-worker campaigns passed: `FuzzHeaderUnmarshalBinary` 60 seconds, 1,374,821 executions (61.575 seconds total); `FuzzFrameUnmarshalBinary` 30 seconds, 564,343 executions (31.603 seconds total), no failures. Only subsequent test assertions changed before the implementation commit, not production. Known unsupported Windows Zig race linking was not retried; supported Linux race and security-patched minimum-Go/platform CI remain external gates. No release, deployment or independent interoperability claim is made.

### Next sequential candidates

Review a complete bounded text/TLV-backed message validation responsibility, preserving public message ownership/current sentinels and the deferred ErrorMessage spec discrepancy. Avoid scalar nonce/random helper packages or duplicate persistent representations merely to increase package count. Session authentication/replay/rekey transactions and runtime lifecycle/handshake lock ownership stay together. Continue only with focused baseline/differential/allocation evidence and promptly committed coherent steps.

## Historical checkpoint: bounded internal TLV codec

Continued from clean `refactor/internal-packages` at `e8d5231`; implementation and coupled tests are committed as `07b85b9`.

```text
dgproto (public concrete API, copies, runtime ownership)
  -> internal/tlv          -> encoding/binary, fmt
  -> internal/replay       -> errors
  -> internal/acceptpolicy -> errors, net, syscall
```

`internal/tlv/codec.go` owns bounded value/sequence/element arithmetic, zero-padded little-endian writing, whole-sequence validation/counting, and the validated second-pass field reader. Its byte/scalar boundary deliberately has no internal entry slice, root alias, unsafe conversion, interface or callback. Root `tlv.go` retains the original concrete TLV, fields, constants, methods and mutable error variables. Root allocates the exact descriptor count after full validation and copies each borrowed value directly once; empty values remain nil, successful empty sequences remain non-nil, malformed suffixes return no partial output. Read/Write preconditions are internal and documented; callers must not concurrently mutate validated input. Error classification is allocation-free and diagnostic formatting wraps the root sentinel selected at the failing call, including replacement or nil sentinels. Size, caller-limit, element, header, value and padding precedence is unchanged.

Golden-wire and alignment-only scenarios move from `tlv_test.go` into `internal/tlv/codec_test.go`, beside overflow, maximum/dense sequence, every-byte truncation, opaque-padding and zero-allocation validation cases. Public identity/method signatures, exact diagnostics/current sentinels, copy separation, nil/empty shape and allocation budgets are in external-package `tlv_api_test.go`. Root `tlv_test.go`, `tlv_decode_limits_test.go`, `tlv_encode_regression_test.go`, `tlv_decode_benchmark_test.go`, vectors, conformance and parser fuzz tests remain integration/public-boundary coverage. No legacy editor filenames are recreated.

### Observed TLV validation and resource evidence

Go 1.26.5 windows/amd64, CGO_ENABLED=1, CC=`zig cc`; module minimum remains 1.25.0 and module/dependency/fixture files are unchanged. Fresh baseline full tests passed. Final focused selector `Test.*TLV|TestBounded|TestValidation|TestCodec|TestJSONWireVectors|TestWIRE|TestHS|TestSESS|TestP0` passed; all parser fuzz seeds also passed before the final scalar-only encoding-boundary adjustment. Uncached `go test -count=1 ./...`, `go test -tags dgproto_keylog -count=1 ./...`, both ordinary/tagged vet, `go build ./...`, `gofmt -l .` and unstaged/staged diff checks passed with definitive exit markers. A temporary AST comparison verified every root type/constant/variable declaration and exported signature against `e8d5231`, and all other root production files byte-identical. An isolated local-replace consumer ran the public API tests, tests/vet, and baseline differential comparison: 10,000 encodes, 20,006 wire cases at five limits, plus constructor/marshal maximum edges; exact error text, output/nil shape, field values and value capacities agreed. Temporary checkers are not repository utilities.

Matched benchmarks used `go test -run "^$" -bench "TLV" -benchmem -benchtime=1s -count=3` before/after; final encoding adjustment was rechecked with the same parameters and `-bench "EncodeTLVs"`. All allocation counts and storage sizes were retained: empty decode 0, tiny 2/40 B, four tiny values 5/160 B, dense empty 1/~524288 B, large single 2/65568 B, malformed 3/120 B; encoding always one allocation for nonempty output (Long 65536 B, Typical 192 B, ManyEmpty 16384 B). An initial record-returning per-field size boundary slowed empty-heavy encoding and was rejected; a scalar failure kind restored final Typical 118.6–126.7 ns/op versus 126.0–136.6 baseline and ManyEmpty 32.3–35.2 microseconds versus 43.6–44.1. Long final 19.5–23.0 microseconds versus 22.8–23.3; decode empty has a small boundary cost (17.3 ns versus 11.7–12.4), while other decode timings were comparable or lower. Timing samples are not statistical performance certification, and some initial post samples overlapped other checks; allocation invariants are independently asserted.

A real `FuzzDecodeTLVs` campaign with `-fuzztime=60s -parallel=2` passed, 1,267,024 executions and no failures (19 baseline corpus entries); final committed boundary was also checked with a 30-second/two-worker campaign: 641,689 executions, no failures, 31.437 seconds total. Supported race remains unavailable on the known Windows Zig toolchain and was not pointlessly retried. Security-patched minimum Go 1.25.x platform CI and supported Linux race remain external gates, not local passes. No independent interoperability, release, protocol/API/cipher/dependency change or fixture regeneration is claimed. The historical ErrorMessage appendix discrepancy remains deferred and untouched.

### Next sequential candidates

This is a substantial complete codec extraction, not a one-helper leaf. Do not add a second speculative package merely for count. Next review the fixed-header wire codec as one bounded responsibility: root must retain Header/Flags/MessageType, mutable Magic, exact validation/error precedence and received-byte AAD; use primitive fixed-array encoding/decoding only if conversion/allocation evidence remains favorable. A bounded text/TLV message validator is another candidate after reviewing shared SessionClose/ErrorMessage ownership and the deferred spec discrepancy; do not silently resolve that discrepancy. Nonce/random helpers alone remain too small. Session authentication/replay/rekey and runtime/handshake atomic ownership stay together. Continue in separately tested, promptly committed cohesive steps, followed by an explicit-path documentation commit and authorized non-force branch push.

## Historical checkpoint: internal replay implementation

Continued from clean `refactor/internal-packages` at `e1ff917`. This stage extracts a real Go package rather than splitting another root source file:

```text
dgproto (public API and runtime ownership)
  -> internal/replay       -> errors
  -> internal/acceptpolicy -> errors, net, syscall
```

`internal/replay/window.go` owns the complete 2048-entry sequence-validation, authenticated commit, and bitmap shift algorithm. Its API operates on uint64 state and a fixed bitmap owned by the caller. `replay.go` keeps the original real root ReplayWindow/ReplayToken types, all fields, method sets, value representation and root sentinel variables. Forwarding methods and error translation retain private helper behavior and the existing nil-receiver boundary. No embedded internal type, pointer-backed shared state, alias, unsafe conversion, interface layer or heap-owned window is introduced. Session's copy-then-commit receive/rekey/grace transaction and its lock stay unchanged; no production file other than `replay.go` changes.

The original `replay_test.go` scenarios move to `internal/replay/window_test.go`. A test-only primitive adapter retains every test name and assertion unchanged, without copying the algorithm or importing root. Public facade regressions stay at root in `replay_api_test.go`: method set, PkgPath/name/field layout/comparability, exact sentinels, wrapped errors.Is/errors.As, replaceable sentinel variables, nil receiver, rejected-state preservation and independent value copies. Existing session/rekey/white-box, vectors and fuzz seed tests remain root integration coverage.

Stable test topic renames (contents unchanged): `tcp_stress_patch_test.go` -> `tcp_concurrent_echo_test.go`, `tlv_decode_patch_test.go` -> `tlv_decode_limits_test.go`, `tlv_encode_patch_test.go` -> `tlv_encode_regression_test.go`. Tests requiring runtime internals remain at root; absent legacy editor paths are not part of this layout.

### Candidate reassessment and next seam

Replay is selected because its complete algorithm depends only on scalar state/bitmap, not Session, transport, crypto, callbacks or locks. Preserving exact root storage costs narrow forwarding and sentinel translation, not a speculative runtime facade. TLV remains a realistic next extraction: an internal codec can own bounded two-pass primitive parsing/encoding while root retains TLV concrete identity, nil/empty value ownership, allocation limits and exact wrapped errors. Review conversion allocation overhead and whether tests can move without losing public ownership coverage before implementing it. Header helpers are presently interleaved with public validation, mutable Magic, flags and exact received-byte AAD; extract a whole useful wire responsibility rather than several tiny byte helpers. Nonce/random-byte helpers alone are too small to justify a crypto package, and moving the full Codec would couple public Frame/Header validation and error precedence. Session/Connection runtime extraction remains a larger separate design because atomic state and lock ownership must not be fragmented. These are scoped follow-ups, not evidence that all internal extraction is unjustified.

### Observed validation

Local toolchain: Go 1.26.5 windows/amd64, CGO_ENABLED=1, CC=`zig cc`. Module and dependencies unchanged. Baseline ordinary tests/vet and actual `dgproto_keylog` tests/vet passed before editing; focused replay/session/rekey/vector/conformance selector passed before and after.

Post-change `go test -count=1 ./internal/replay`, `go test -run "Test.*(Replay|Rekey|Session)|TestJSONWireVectors|TestWIRE|TestHS|TestSESS|TestP0" -count=1 ./...`, `go test -count=1 ./...`, `go vet ./...`, and `go test -tags dgproto_keylog -count=1 ./...` passed. `go vet -tags dgproto_keylog ./...`, `go build ./...`, `gofmt -l .`, and unstaged/staged `git diff --check` also completed with an exit-0 marker and no diagnostics/format filenames. Changed Go files alone were gofmt-written. Modified documentation link targets and fence balance were checked; the staged diff was empty at the original extraction validation checkpoint. A temporary AST checker verified all root type/constant/variable definitions and exported signatures against the original `e1ff917` HEAD, unchanged other production files, exact renamed-test contents and all relocated replay scenarios/assertions. A temporary external module with local replace ran the public API tests and 100,000 deterministic operation iterations against the original HEAD replay implementation, comparing errors, bitmap/highest/generation, stale commits and value copies; consumer tests and vet passed. Temporary verification code is not shipped.

Supported race remains unavailable, not passed: fresh compiler discovery finds Zig only, with no gcc/clang; the known Windows linker failure on WaitOnAddress/WakeByAddressSingle/WakeByAddressAll is not retried. Security-patched minimum Go 1.25.x platform CI and supported Linux race remain external gates. No dependency update, fixture regeneration, fuzz campaign, benchmark or interoperability certification. At the original extraction checkpoint, no staging, commit, push, history changes or release artifacts were authorized. The user subsequently explicitly authorized commit/push for this ongoing refactor session: checkpoint work is committed in thematic replay, pure-test-rename, and architecture/workflow groups with explicit path staging; this does not authorize release artifacts or future unrelated remote actions. The ErrorMessage appendix discrepancy documented below remains deferred and untouched.

For development, add replay algorithm tests beside `internal/replay/window.go`, public contract tests at root, and use the focused selector above followed by uncached full tests/vet and `dgproto_keylog` checks. Follow the [contribution guide](../../.github/CONTRIBUTING.md#where-to-add-code-and-tests). Do not resume by repeating a historical root-only split.

## Historical checkpoint: block 9 implemented, handshake message family

Continued from clean `refactor/internal-packages` at `c7d9cbe`. Implemented the previously recommended cohesive root source boundary: moved NoisePattern, its XX/IK constant group, all three handshake wrapper structs and their six methods (11 complete declarations) from `messages.go` to `messages_handshake.go`. The new file imports only `fmt`; shared sizes/error sentinels and application/control families stay in `messages.go`. No alias facade, new internal package, state transfer, module dependency, wire, crypto or keylog change. This is source/test organization within the public library, not an application/server hierarchy.

Fresh graph inspection still finds exactly root → `internal/acceptpolicy` → standard library; root also uses Noise and x/crypto. Public wire/message/replay definitions and sentinels must retain root concrete identity and representation. Moving coupled runtime/session state internally would either change that identity or require a speculative facade/interfaces across locks and atomic epoch/replay transitions. The handshake family instead has a real navigation boundary without changing ownership. The prior block-8 inventory below is historical; `messages.go` now loses 125 lines and the new cohesive wrapper file has 128 lines.

Added external-package `messages_handshake_test.go`: compile-time binary codec method-set checks, reflection of root concrete type identity (rejects internal aliases), sentinel/wrapped `errors.Is`/`errors.As` behavior, and final-flight copy ownership plus every invalid truncated length/one-byte excess and unchanged destination on rejection. Existing mixed message tests, deterministic vectors, conformance, handshake/client/server integration and keylog tests remain intact. Add wrapper code/tests in this family; keep cross-family vector/fuzz tests at root. See the updated [contribution guide](../../.github/CONTRIBUTING.md#where-to-add-code-and-tests) for public/internal dependency direction and test placement.

Observed locally on Go 1.26.5 windows/amd64, CGO_ENABLED=1, CC=`zig cc`:

- Fresh `go test -count=1 ./...` baseline PASS (root 2.185 s, internal 0.539 s).
- Baseline/post `go test -run "Test.*(Handshake|Message|Client|Server)|TestJSONWireVectors|TestWIRE|TestHS|TestSESS|TestP0" -count=1 ./...` PASS (root 1.329 s / 1.308 s); actual selected names listed before editing.
- Post `go test -count=1 ./...` PASS (root 2.244 s); `go test -tags dgproto_keylog -count=1 ./...` PASS (root 2.184 s).
- `go vet ./...`, `go vet -tags dgproto_keylog ./...`, `gofmt -l .`, `git diff --check` PASS with no diagnostics/filenames; only changed Go files formatted. Final staged diff remains empty; relative file links, new contribution anchors and Markdown fence balance checked.
- Temporary Go parser/source-slice multiset check PASS: all 247 complete non-import production declarations including comments, bodies, type/field definitions and sentinels identical to HEAD after CRLF normalization; exact 11-declaration destination. Module files and keylog files unchanged. The checker is outside the repository, not a shipped utility.

Race execution remains unavailable, not PASS: fresh discovery still finds only Zig, no gcc/clang; the prior observed Windows external-link failure on WaitOnAddress/WakeByAddressSingle/WakeByAddressAll is unchanged, so the unavailable gate was not retried. Supported Linux race and security-patched minimum Go 1.25.x Linux/Windows/macOS execution remain external requirements. No fuzz campaign, benchmark, dependency upgrade/scan, independent interoperability or production certification is claimed. No exported alias/type wiring changed, so a separate local-replace consumer smoke is not required; external-package tests exercise the public import/method/type/error contracts.

This checkpoint is an uncommitted working-tree change: no staging, commit, push, reset, clean or release artifacts are authorized. Resume by freshly reading guides, status/diffs and current code; do not repeat block 9 or follow historical commit/staging instructions below. Further extraction requires its own cohesive ownership/dependency justification, not file-count goals.

## Boundaries and staged roadmap

The root `dgproto` package remains the public API and owns its exported definitions. File size alone does not justify a new package. Prefer cohesive root files before inventing interfaces or moving state across ownership boundaries.

| Block | Scope | Gate to proceed |
|---|---|---|
| 1 (landed) | Extract the stateless accept-error classifier to `internal/acceptpolicy`; retain the root wrapper and runtime tests. | Direct/wrapped regression parity and default/tagged package checks. |
| 2 (landed, separate commit) | Root-only connection send/queue implementation split into `connection_send.go`; types and white-box tests stay at root. | Preserve enqueue, cancellation, writer/rekey ordering, completion and shutdown contracts. |
| 3 (landed, separate commit) | Root-only keepalive/maintenance split into `connection_keepalive.go`. | Preserve timer reset, ping acknowledgement, idle timeout and cancellation behavior with focused liveness tests and race execution. |
| 4 (landed, separate commit) | Root-only lifecycle split into `connection_lifecycle.go`. | Preserve start/close/abort ownership, first cause, handler completion and server wait boundaries. |
| 5 (landed, separate commit) | Root-only responder establishment split into `server_handshake.go`; client shares the root frame validator. | Exact four-declaration relocation, handshake/ownership baseline and post/full/tagged checks. |
| 6 (landed, separate commit) | Root-only connection receive and handler dispatch into `connection_receive.go`. | Preserve control routing, queue/cancellation, bounded handler and terminal ownership; exact declaration equivalence. |
| 7 (landed, separate commit) | Root-only session outbound/rekey transaction into `session_send.go`. | Preserve complete send declarations, state/lock/sequence/ownership and rekey ordering; parser and focused/full/tagged gates. |
| 8 (landed) | Cohesive root-only session receive transaction into `session_receive.go`. | All six declarations exact; focused/default/tagged/vet/parity passed; supported race remains blocked externally, not passed. |
| Later reassessment | Reconsider real internal seams only after root cohesion improves. | Prove a one-way dependency graph and explicit state/buffer ownership; reject alias facades and speculative abstractions. |

`ReplayWindow` is public: moving its definition or substituting an alias risks public type identity, representation and white-box state assumptions. It is not a reason to expose a new internal state type. TLV, headers, frames and typed messages are also public and carry copy/alias, field layout, byte encoding and error contracts; a wire package would require a separately reviewed compatibility design, not a mechanical move. `Session`, replay and rekey share locking and atomic receive transitions: authentication failure must not advance replay, malformed ordinary authenticated messages consume sequence, and rekey validation/key/epoch/replay/grace updates must remain atomic. Keep them together now. Preserve root sentinels and `errors.Is`/`errors.As`, public identity/representation, defaults, reachable-buffer ownership, queue bounds and cancellation semantics in every block. No new public types, aliases, dependencies, crypto algorithms or wire changes are part of this roadmap.

## Historical block 1 checkpoint: first stateless leaf

- `internal/acceptpolicy/errors.go` exports only `Retryable(error) bool` within an internal package; production imports are exactly `errors`, `net`, `syscall`, with no root dependency.
- `server.go` retains unexported `isTemporaryAcceptError` as a forwarding compatibility wrapper. `Serve` remains its sole production caller. Backoff starts at 5 ms, doubles up to 1 s, and shutdown/timer/admission ownership remains in the server.
- The classifier algorithm and explanatory criteria were relocated unchanged: `errors.As` finds timeout `net.Error`; `errors.Is` matches `ECONNABORTED`, `EMFILE`, `ENFILE`; the synthetic `IsTemporary` marker is a direct structural assertion only. A wrapped marker alone is false; deprecated `Temporary()` alone is false. No Windows errno substitutions or broadened retries.
- Root classifier regression cases were added and observed passing before production relocation. Existing root retry and close/backoff tests stay in place. Independent internal tests cover direct/wrapped timeout, non-timeout, errno, permanent, closed, deprecated-Temporary and direct-marker cases.
- The existing close/backoff test does not synchronize entry into the timer select; its passing result is not deterministic timer-entry proof. Strengthening lifecycle tests is deferred, not silently bundled here.
- At block 1 no subsequent roadmap block had been implemented. Later landed blocks and the current resume point are recorded below. No module, fixture, keylog, wire or exported definition changes belong to this checkpoint.

### Observed local validation

Toolchain: `go version` reported `go1.26.5 windows/amd64`; `go env GOOS GOARCH CGO_ENABLED` reported `windows`, `amd64`, `1`. Module minimum remains Go 1.25.0. Commands completed with explicit exit markers, not inferred from starting a background process.

| Command | Observed outcome |
|---|---|
| `go test -run '^(TestIsTemporaryAcceptError\|TestServerServeRetriesTemporaryAcceptError\|TestServerServeCloseDuringTemporaryBackoff)$' -count=1 .` (before relocation) | PASS, exit 0; root 1.214 s. |
| `go test -run '^(TestIsTemporaryAcceptError\|TestServerServeRetriesTemporaryAcceptError\|TestServerServeCloseDuringTemporaryBackoff\|TestRetryable\|TestRetryableDirectMarker)$' -count=1 ./...` (after relocation) | PASS, exit 0; root 1.264 s, internal 0.293 s. |
| `go test -count=1 ./...` | PASS, exit 0; root 2.199 s, internal 0.505 s. |
| `go test -tags dgproto_keylog -count=1 ./...` | PASS, exit 0; root 2.285 s, internal 0.303 s. |
| `go vet ./...` | PASS, exit 0; no diagnostics. |
| `gofmt -l .` | PASS, exit 0; no filenames. Changed Go files were formatted individually. |
| `git diff --check` | PASS, exit 0, including final prose review; repeat for staged content before commit. |
| `go list -f '{{.Imports}}' ./internal/acceptpolicy` | PASS, exit 0; `[errors net syscall]`. |
| `go test -race -count=1 ./...` | BLOCKED: exit 1 at external linking for both root and internal test binaries; no race tests executed. |

The configured Zig C linker could not resolve `WaitOnAddress`, `WakeByAddressSingle`, and `WakeByAddressAll` referenced by the Go race runtime. This is a local race-toolchain/linking limitation, not an observed test assertion failure or proof of race safety. No flags, source changes, reduced checks or compiler substitutions were applied to hide it. Rerun the exact race command with a supported C/race toolchain (including the configured Linux CI gate).

Go 1.26.5 Windows results are not minimum-line Go 1.25.x or Linux/macOS execution evidence. Those executions remain required elsewhere; configured CI is not an observed pass. Fuzz campaigns, benchmarks, vulnerability/dependency scans and independent interoperability/deployment checks were not run for this behavior-preserving leaf relocation. There is no production certification. All seven relative links in the changed architecture documents were checked against existing files; `DGPROTO_KEYLOG` was unset for the ordinary checks. Final staged content must still be reviewed before committing; discover the checkpoint commit with `git log -1 -- docs/architecture/package-layout.md` rather than embedding its own hash here.

### Verified discrepancy, deferred separately

The protocol document's final **Implementation constraints and application responsibilities** appendix describes `ErrorMessage` (`0x09`) as `code:u16 little-endian || context:utf8`. In [messages.go](../../messages.go), `ErrorMessage.MarshalBinary` calls `marshalTextMessage`, which writes the two-byte LE code and, for non-empty context, a `textTLVType` TLV via `NewTLV`/`MarshalBinary`; `unmarshalTextMessage` decodes that TLV. Empty context is just the two code bytes. Thus raw UTF-8 appendix wording does not describe the implemented non-empty payload representation. Resolve normative intent, existing vectors/tests and compatibility in a separate wire/documentation review; do not change encoding or silently bless the discrepancy in this reorganization.

## Block 2 landed: send and queue cohesion

`connection_send.go` contains 14 unchanged functions: `Send`, `TrySend`, `SendContext`, `SendPaddedContext`, `SendAndWait`, `SendPaddedAndWait`, `enqueue`, `enqueueControl`, `enqueueChannel`, `SendPadded`, `writeLoop`, `complete`, `failPending`, `writeMessage`. All files remain `package dgproto`. Central exported definitions, private channel item types, sentinels and constructor remain in `connection.go`; white-box tests are untouched. Shared `operationContext` stays with the reader/handler runtime, since both read and write use it. No queue, cancellation, rekey, lock, buffer or completion behavior changed.

Fresh block 2 checks on Go 1.26.5 windows/amd64, CGO enabled, `DGPROTO_KEYLOG` unset:

- Before and after: `go test -run 'Test.*(Send|Queue|Shutdown|Close|Rekey)' -count=1 ./...` PASS, exit 0 (root 1.401 s / 1.400 s).
- `go test -count=1 ./...` PASS, exit 0 (root 2.163 s).
- `go test -tags dgproto_keylog -count=1 ./...` PASS, exit 0 (root 2.190 s).
- `go vet ./...`, `gofmt -l .`, `git diff --check` PASS, exit 0; no vet/format diagnostics. Only changed Go files were formatted.
- Temporary Go parser validation compared the sorted complete non-import declaration source slices, including declaration comments and bodies: all 42 declarations across the connection files exactly match pre-block HEAD (normalized CRLF only). This covers exported type definitions/representation, methods/signatures, constructor, private types, sentinels and unmoved functions; no aliases or interfaces introduced.
- Fresh `go test -race -count=1 ./...` BLOCKED, exit 1 at external linking, no tests executed: configured Zig cannot resolve `WaitOnAddress`, `WakeByAddressSingle`, `WakeByAddressAll`. Compiler discovery found only Zig among available gcc/clang/zig commands; no system or dependency changes made. This same environmental limitation applies to the subsequent mechanical moves; race safety is not claimed. Minimum Go 1.25.x and Linux/macOS execution remain unavailable locally.

Block 2's proposed keepalive boundary was implemented in block 3 below. Discover actual checkpoint hashes using Git history, not an embedded self-hash.

## Block 3 landed: keepalive and maintenance cohesion

`connection_keepalive.go` contains 8 unchanged functions: `defaultKeepaliveTimeout`, `beginPing`, `clearPing`, `acknowledgePing`, `consumePingAcknowledgement`, `maintenanceLoop`, `noteActivity`, `resetTimer`. The overflow-safe default moves with its timer policy; constructor/config declarations stay central. The reader still authenticates and forwards Ping/Pong handling, while maintenance owns timer sequencing. Ping state, atomics, locks, channels, ACK-before-enqueue retention, rollover, retry and deadline behavior are unchanged. Tests remain at root.

Fresh block 3 checks, same toolchain/platform and environment as block 2:

- Before/after: `go test -run 'Test.*(Keepalive|Ping|Idle|Send|Queue|Shutdown|Close|Rekey)' -count=1 ./...` PASS, exit 0 (root 1.848 s / 1.869 s), including authenticated ACK burst, immediate reply, enqueue interleaving, queue-pressure retry and state-boundary tests.
- `go test -count=1 ./...` PASS, exit 0 (root 2.250 s).
- `go test -tags dgproto_keylog -count=1 ./...` PASS, exit 0 (root 2.202 s).
- `go vet ./...`, `gofmt -l .`, `git diff --check` PASS, exit 0; no diagnostics. Only `connection.go` and the new keepalive file formatted.
- Temporary parser/source-slice equivalence PASS: all 42 non-import connection declarations, bodies and declaration comments unchanged vs pre-block HEAD; public definition identity and representation unchanged.
- Race not rerun: the fresh block 2 attempt failed externally before tests on the unchanged Zig toolchain (three missing Windows address-wakeup symbols above). Race execution and minimum-line/other-OS evidence remain required elsewhere, not claimed passes.

Block 3's proposed lifecycle boundary was implemented in block 4 below.

## Block 4 landed: all three connection root splits complete

`connection_lifecycle.go` contains 8 unchanged functions: `Start`, `Close`, `Done`, `Err`, `closeGracefully`, `shutdown`, `recordTerminal`, `abort`. `connection.go` retains all central connection/config/error declarations, private item types and `NewConnection`, plus `readLoop`, `handlerLoop`, `callHandlerBounded`, `operationContext`, `callHandler` (6 functions total). Reader/dispatch and bounded handler execution are distinct responsibilities, so no extra extraction was needed for lifecycle cohesion. Loop installation/joining, caller-owned stopped runtime, close frame/result priority, cancellation, first cause, shutdown leases, queue disposal, callback bounds and Done ownership remain unchanged. No test, module, wire, crypto or dependency edits.

Fresh block 4 validation on the same Go 1.26.5 Windows toolchain:

- Baseline/post-move: `go test -run 'Test.*(Start|Close|Shutdown|Handler|Disconnect|Admission|Send|Queue|Rekey|Keepalive|Ping|Idle|Abort|Handoff|Terminal|Lifecycle)' -count=1 ./...` PASS, exit 0 (root 1.870 s / 1.948 s). Coverage includes close-before-start, concurrent/pre-canceled start-close, handler error/panic/bounded shutdown, terminal-cause stability and server ownership/handoff tests; full tests also cover callback tests whose names are outside the selector.
- `go test -count=1 ./...` PASS, exit 0 (root 2.306 s).
- `go test -tags dgproto_keylog -count=1 ./...` PASS, exit 0 (root 2.169 s).
- `go vet ./...`, `gofmt -l .`, `git diff --check` PASS, exit 0; no diagnostics. Only `connection.go` and `connection_lifecycle.go` formatted.
- Temporary Go parser/source-slice equivalence PASS against both pre-block HEAD and the original three-block base `74846e9d47b635ca0d6bf8e7ad81e4c968b22746`: all 42 complete non-import declarations identical, including all 36 function bodies/signatures/comments and 6 central type/sentinel declarations. Source normalization is CRLF only; declarations are compared as a sorted multiset, so duplicate/missing declarations fail. Public definitions are still real root definitions, not aliases/facades.
- Race remains environmentally blocked as freshly observed in block 2; not rerun on the unchanged toolchain for blocks 3/4, and no race pass claimed. Supported race execution plus Go 1.25.x Linux/Windows/macOS remain required elsewhere. Fuzz campaigns, benchmarks, dependency/vulnerability scans and interoperability/deployment checks were not run for these mechanical moves.

Each landed block has its own focused baseline, post-move/full/tagged checks and exact-path staged review before a separate local commit. Check actual hashes with `git log -3 --oneline` and actual status/diffs before resuming; do not trust cached state. No push, tag, release or release-note/candidate artifacts are part of these blocks. Earlier wire appendix and nondeterministic close/backoff test findings remain deferred as documented above.

## Historical checkpoint: block 5 landed, server establishment cohesion

Freshly verified `refactor/internal-packages` at `6cc4bf8fb3f2bc1abbcd30d3687baa05dfee2ce7`, clean tracked/untracked status and empty full staged/unstaged diffs before editing. Full repository guide and checkpoint, implementation, callers, relevant tests and handshake specification inspected. Exactly `(*Server).performHandshake`, `validateHandshakeFrame`, `isClientAllowed`, `hmacEqual` moved to `server_handshake.go`, still `package dgproto`; its only imports are `context` and `fmt`. All original `server.go` imports remain necessary. The validator is the same root function called by the client, with no wrapper, alias or new abstraction. Server/config/errors/constructor/Serve/handleConn/admission/disconnect/slots/register/handoff/Close/Abort/backoff remain in `server.go`. No tests, dependencies, API, wire, security policy, algorithms, locking or behavior changed. The known ErrorMessage appendix discrepancy remains deferred.

Observed fresh validation, Go 1.26.5 windows/amd64, CGO enabled, `DGPROTO_KEYLOG` unset; completion and exit checks read explicitly:

- Baseline/post: `go test -run 'Test.*(Server|DialContext|Handshake|Admission|Handoff|WorkerReservation|Abort)' -count=1 ./...` PASS, exit 0 (root 1.388 s / 1.459 s). Includes allowlist rejection, malformed flights, handshake and connection saturation/release, stalled-handshake Close, admission socket ownership, worker reservation, handoff shutdown stress and DialContext pin/final-write/cancellation/stopped-ownership tests.
- `go test -count=1 ./...` PASS, exit 0 (root 2.270 s, internal 0.562 s).
- `go test -tags dgproto_keylog -count=1 ./...` PASS, exit 0 (root 2.223 s, internal 0.521 s).
- `go vet ./...`, `gofmt -l .`, `git diff --check` PASS, exit 0, no diagnostics. Only `server.go` and `server_handshake.go` formatted.
- Temporary Go parser checks before and after PASS: sorted multiset of all 247 complete non-import root production declarations equals pre-block HEAD, including doc comments, bodies, signatures, types and sentinels; CRLF is the only source normalization. Additional exact-scope check verifies precisely the four named functions in the new root file and none remaining in `server.go`. Temporary checker lives outside the repository and is removed before completion.
- Race deliberately not retried without a new supported compiler: the fresh prior Zig Windows linker failure (`WaitOnAddress`, `WakeByAddressSingle`, `WakeByAddressAll`) prevented test execution, and no installed gcc/clang alternative was found. This is deferred supported `go test -race -count=1 ./...` evidence, not a race pass. Minimum-line Go 1.25.x execution on Windows/Linux/macOS, Linux supported race and other configured CI gates remain required elsewhere; local newer-Go Windows checks do not substitute for them. No push/tag/release or release-note/candidate files.

## Historical checkpoint: block 6 landed, connection receive cohesion

Started from freshly verified clean `refactor/internal-packages` at `8d9b31653790aa0b86e4ee4654763dfebc48d76d`; full staged/unstaged diffs empty. Moved exactly `(*Connection).readLoop`, `handlerLoop`, `callHandlerBounded` (complete explanatory comment), `callHandler` into root `connection_receive.go`, importing only `fmt`, `time`. Central types/errors/constructor/shared `operationContext` stay in `connection.go`, which loses only unused `fmt`. Queue leases, control routing, cancellation rechecks, bounded detachment, panic containment and shutdown semantics are unchanged. No tests rewritten.

Fresh Go 1.26.5 windows/amd64, CGO 1, configured `zig cc`; compiler discovery still finds only Zig, no gcc/clang alternative. `DGPROTO_KEYLOG` remains unset for ordinary checks. Explicit completion markers read:

- Baseline/post `go test -run 'Test.*(Connection|Handler|Queue|Shutdown|Close|Start|Keepalive|Ping|Rekey|Admission|Handoff|Abort)' -count=1 ./...`: PASS exit 0; root 1.965 s / 2.013 s. Actual selected names inspected using `go test -list`.
- `go test -count=1 ./...`: PASS exit 0; root 2.158 s, internal 0.526 s.
- `go test -tags dgproto_keylog -count=1 ./...`: PASS exit 0; root 2.193 s, internal 0.282 s.
- `go vet ./...`, `gofmt -l .`, `git diff --check`: PASS exit 0, no diagnostics/filenames. Formatted only the two changed Go files.
- Temporary parser: all 247 complete non-import root production declarations identical as a sorted multiset to pre-block HEAD and cumulative original `74846e9d47b635ca0d6bf8e7ad81e4c968b22746`, including comments/bodies/types/signatures, CRLF-only normalization. Build-tag files byte-unchanged; exact new-file four-function scope and absence from central file checked.
- Race not rerun on unchanged environment: previous fresh Zig Windows link failure prevented execution (missing `WaitOnAddress`, `WakeByAddressSingle`, `WakeByAddressAll`). Supported race and minimum Go 1.25.x Windows/Linux/macOS execution remain external requirements, not passes. No fuzz campaign/benchmark needed or run for unchanged-source relocation. No push/tag/release or release-note/candidate documents.

Historical block 6 maps and exact five-path full/staged review preceded its separate local commit. Its then-next session outbound scope subsequently landed in block 7; it is not the current continuation point.

## Historical checkpoint: block 7 landed, session outbound cohesion

After block 6's separate local commit `31655a85dee7ce3ea9c174b47c6c20086e21c681`, freshly verified branch/HEAD, clean status and empty full diffs. Moved exactly `(*Session).Send`, `SendPayload`, `sendPayload`, `MarkRekeySent`, `encryptLocked`, `rekeyDueLocked`, `startRekeyLocked`, `sameRekeyFrame` into root `session_send.go`, importing only `fmt`, `math`. Complete declaration comments and source unchanged. `Session`, `HandshakeSecrets`, `pendingSendRekey`, sentinels/constructors/close, receive candidate/transitions and shared dispatch remain central. All original imports remain needed, including `math` in `prepareRekeyLocked`. No wire/state/lock/sequence/key/padding/ownership/rekey changes; no tests rewritten, new abstractions or dependencies.

Observed fresh checks on the same Go 1.26.5 Windows/amd64 CGO-enabled toolchain, ordinary keylog environment unset; explicit completion markers read:

- Baseline/post `go test -run 'Test.*(Session|Rekey|Send|Queue|Shutdown|Close)' -count=1 ./...`: PASS exit 0; root 1.360 s / 1.414 s.
- Baseline/post `go test -run 'TestJSONWireVectors|TestWIRE|TestHS|TestSESS|TestP0' -count=1 ./...`: PASS exit 0; root 1.208 s / 1.273 s. Listed actual names first: JSON vectors and all four conformance prefixes exist.
- `go test -count=1 ./...`: PASS exit 0; root 2.211 s, internal 0.546 s.
- `go test -tags dgproto_keylog -count=1 ./...`: PASS exit 0; root 2.192 s, internal 0.282 s.
- `go vet ./...`, `gofmt -l .`, `git diff --check`: PASS exit 0, no diagnostics/filenames. Formatted only `session.go` and `session_send.go`.
- Strict temporary Go parser/source-slice equivalence PASS: all 247 complete non-import root production declarations identical to pre-block HEAD and cumulative original `74846e9d47b635ca0d6bf8e7ad81e4c968b22746`, as a sorted multiset with only CRLF normalization. Includes types/signatures/bodies/comments/sentinels, exact eight-function destination scope, no moved functions remaining centrally, build-tag files byte-unchanged.
- Unchanged Zig race-link limitation not retried; supported `go test -race -count=1 ./...`, security-patched Go 1.25.x Windows/Linux/macOS and supported Linux race remain external execution requirements. Fuzz campaigns, benchmarks, dependency/vulnerability scans and interoperability checks not performed for mechanical unchanged-source moves. Deferred ErrorMessage appendix mismatch remains untouched.

Exact five-path full/staged content review and whitespace checks precede this separate local commit; discover its hash in Git history. No push/tag/release, release-note/candidate files or third source block. Temporary parser lives outside the repository and is removed before completion.

## Historical block 8 checkpoint: session receive and reassessment (superseded by block 9)

Started from freshly verified clean `refactor/internal-packages` at `5c9000c34e62e192ce1df7c50e73031f8461b6a3`, with empty full staged/unstaged diffs. Full repository guide and checkpoint read; no nested guides found. Moved exactly five `*Session` methods (`Receive`, `ReceivePayload`, `decryptEpochLocked`, `prepareRekeyLocked`, `expireGraceLocked`) and the private `receiveCandidate` struct to root `session_receive.go`. Complete comments, signatures and bodies are unchanged. Imports there are exactly `crypto/hmac`, `errors`, `fmt`, `math`, `time`; central `session.go` loses only proven-unused `crypto/hmac` and `math`. Public/state/pending types, errors, constructors, Close/Closed and shared message dispatch remain central. Authentication, replay, epoch/key/rekey/grace commit and lock ownership remain together. No tests, API, dependencies, algorithms, crypto or wire changes.

### Current observed checks (earlier block evidence below/above is historical)

Go 1.26.5 windows/amd64, CGO 1, CC `zig cc`, ordinary `DGPROTO_KEYLOG` unset. Actual test names verified with `go test -list`, including replay token/boundary tests, rejected-rekey atomicity, authentication failure, malformed authenticated message consumption and all requested conformance prefixes.

| Check | Observed result |
|---|---|
| Baseline/post `go test -run 'Test.*(Session\|Rekey\|Replay\|Send\|Queue\|Shutdown\|Close)' -count=1 ./...` | PASS; root 1.434 s / 1.377 s. |
| Baseline/post `go test -run 'TestJSONWireVectors\|TestWIRE\|TestHS\|TestSESS\|TestP0' -count=1 ./...` | PASS; root 1.221 s / 1.185 s. |
| `go test -count=1 ./...` | PASS exit 0; root 2.240 s, internal 0.518 s. |
| `go test -tags dgproto_keylog -count=1 ./...` | PASS exit 0; root 2.217 s, internal 0.292 s. |
| `go vet ./...` and `go vet -tags dgproto_keylog ./...` | PASS exit 0; no diagnostics. |
| `gofmt -l .`, `git diff --check` | PASS exit 0; no format filenames/whitespace errors; gofmt writes only the two changed source files. |
| Strict temporary Go parser before/after | PASS: sorted multiset of all 247 complete non-import root production declarations exactly equals pre-block HEAD and original `74846e9d47b635ca0d6bf8e7ad81e4c968b22746`; only CRLF normalization. Comments, bodies, signatures, source types and sentinels included, duplicates/missing declarations rejected. Exact six-declaration destination and absence centrally verified. |
| Keylog bytes | All four existing keylog production/tag/test files byte-identical to both baselines; no normalization for this check. |
| `go list -f '{{.ImportPath}} {{.Imports}}' ./...` | Exactly root plus `internal/acceptpolicy`; leaf imports only `errors net syscall`, no root dependency. |

Post-check command chain completed with explicit exit-0 marker. Baselines additionally rerun from an isolated archive of pre-block HEAD: both completed with explicit return codes `[0, 0]` (root 1.408 s / 1.263 s); temporary snapshot removed. Document links/fences verified. Full/staged review and staged whitespace checks are required before the one exact-path local commit. The temporary parser is outside the repository and removed before completion; discover this checkpoint's own hash through Git history rather than embedding a self-hash.

Supported race remains **NOT PASS**: unchanged compiler discovery finds only Zig, no gcc/clang. The previously observed external-link failure on `WaitOnAddress`, `WakeByAddressSingle`, `WakeByAddressAll` prevented race test execution. Do not repeat the unavailable gate without a changed environment; supported `go test -race -count=1 ./...`, minimum-line security-patched Go 1.25.x Windows/Linux/macOS and Linux race remain external requirements. No fuzz campaigns or benchmarks performed or needed for pure unchanged-source relocation; no interoperability, security/dependency scan or deployment certification claims. ErrorMessage appendix/implementation mismatch and nondeterministic backoff-test entry evidence remain deferred, not repaired.

### Final layout and improvement

Parser-observed root inventory: **24 production Go files, 171 functions, 247 non-import declarations**, including both keylog build variants; 63 root Go files including tests. Original refactor baseline has 17 production files, so file count increased by seven cohesive root splits. Directory boundaries define Go packages: this is improved navigation within `dgproto`, not seven internal package extractions. `internal/acceptpolicy` remains the only true stateless leaf. Other directories are guides/assets/protocol, workflows and deterministic fixtures, not production packages.

| Root unit | LF-normalized lines / functions | Current responsibility |
|---|---|---|
| `connection.go` | 134 / 2 | Definitions, constructor, shared operation context. |
| `connection_send.go` | 193 / 14 | Queue admission, writer/rekey ordering, completion/disposal. |
| `connection_keepalive.go` | 152 / 8 | Timers, ping ACK and activity policy. |
| `connection_lifecycle.go` | 150 / 8 | Start, close, terminal cause and completion ownership. |
| `connection_receive.go` | 127 / 4 | Reader/control routing and bounded serial handler. |
| `session.go` | 220 / 9 (pre-block 397 lines) | Real central types/state/errors, construction/close/shared codec dispatch. |
| `session_send.go` | 152 / 8 | Complete outbound/rekey transaction. |
| `session_receive.go` | 184 / 5 plus private candidate type | Complete authentication/replay/epoch/rekey/grace transaction. |
| `server.go`, `server_handshake.go` | 453 / 15; 101 / 4 | Central runtime ownership and responder establishment respectively. |
| `messages.go` | 480 / 23 | Typed families and shared validation/text codec. |
| `client.go`, `tcp.go`, `transport.go` | 174 / 4; 177 / 7; 34 / 0 | Dial ownership, framed I/O with cancellation/deadlines, interfaces/errors. |

Connection central code fell from the historical 256-line pre-receive unit to 134 lines; Session central code from 543 lines before directional splits to 220. The receive split removes 175 complete declaration/comment lines plus two imports centrally; destination imports account for added navigation overhead. Existing root tests/white-box state stay intact. Do not subdivide these completed transaction files merely to reduce line counts.

### Next optional boundaries (not implemented; stop after this commit)

1. **Recommended next mechanical scope: handshake message family.** If continuing after fresh reassessment, move only `NoisePattern` and its XX/IK constant declaration, `HandshakeInit`, `HandshakeResponse`, `HandshakeFinish`, and all six corresponding MarshalBinary/UnmarshalBinary methods (complete comments and real root definitions) into root `messages_handshake.go`. Keep shared fixed-size constants/errors and all other families/helpers in `messages.go`; no aliases or internal package. This removes an approximately 130-line cohesive three-flight wrapper family, larger and more defensible than one-helper files. Inventory exact AST declarations first; type/field/comment identity and copy/length/alignment checks must remain byte-source equivalent. Keep EncryptedData with duplicate-TLV validation and SessionClose/ErrorMessage with their shared text codec for now. Require focused message/handshake/client/server/vector/conformance baseline and post checks, uncached default/keylog, both vet configurations, format/diff/staged and whole-root declaration/keylog-byte parity; supported race/platform status explicit. This is a possible separate block, not work done here.
2. **Deferred larger ownership boundary: server runtime.** `handleConn`, `callAdmission`, `callDisconnect`, slot helpers, handshake registration/worker reservation and connection registration/atomic handoff form a substantial server-owned cluster. A later root `server_runtime.go` could group them, but only after reviewing `Serve`, `beginShutdown`, Close/Abort together: all share `mu`, `wg`, slot leases and callback ownership. Moving just handleConn or creating an internal server facade obscures these invariants. Keep types/config/errors/constructor and shared shutdown coordination root; require admission/handoff/worker reservation/slot release/shutdown tests and supported race evidence for any nonmechanical ownership change. Prefer the message-family relocation before this higher-risk boundary.

Transport lane acquisition, watchContext cancellation-worker join/deadline reset and transportError are only a small cohesive group within 177-line `tcp.go`; moving them now adds files without a meaningful ownership gain. An internal wire/codec package is also deferred: public Header/Frame/TLV/message definitions, errors and mutable-buffer ownership require a compatibility/dependency design, not aliases or invented interfaces. No extra package exists solely for appearance.

Resume from the latest clean committed HEAD, reread full guides and this **current** section, inspect full staged/unstaged diffs and exact scope. All eight roadmap blocks are landed; earlier plans and validation below/above are historical, not current instructions to redo session send/receive. No push/tag/release or release-note/candidate files; no second source block in this invocation.

## Historical block 5 read-only reassessment and executable boundaries (superseded by current checkpoint)

Source inventory below uses parser-observed LF-normalized bytes/line counts; CRLF checkout bytes may differ. This is browsing/cohesion evidence, not a size threshold for extracting packages.

| File | Bytes / lines | Responsibility clusters |
|---|---|---|
| `server.go` | 13,682 / 453 (was 16,392 / 548) | Central definitions; constructor validation/defaults; accept loop/backoff; Close/Abort; handleConn admission/runtime ownership; slots/register/handoff. Retain cohesion now. |
| `server_handshake.go` | 2,756 / 101 | Four responder-establishment/shared validation/allowlist functions. Complete this boundary, no further moves here. |
| `connection.go` | 7,611 / 256 | Five central types plus errors; constructor (100–128); receive/control routing (130–177); serial/bounded/panic-contained handler execution (179–240, 249–256); shared operation context (242–247). |
| `connection_send.go`, `connection_keepalive.go`, `connection_lifecycle.go` | 5,752 / 193; 3,490 / 152; 4,056 / 150 | Already cohesive queue/writer, liveness policy and lifecycle boundaries. Do not subdivide them. |
| `session.go` | 16,831 / 543 | Central secrets/state/construction/close (through 140); outbound/rekey (142–297); atomic receive/replay/rekey (299–472); shared codec/type dispatch (474–543). |
| `messages.go` | 17,209 / 480 | Registry/constants; handshake wrappers; EncryptedData; reserved ResumptionTicket; PingPong/Ack/RekeyInit; SessionClose/ErrorMessage and shared text-TLV codec. Family separation needs full type/comment/codec review; no microfiles now. |
| `client.go`, `tcp.go`, `transport.go` | 6,750 / 174; 4,557 / 177; 1,084 / 34 | Client config/normalization/dial ownership/final flights; transport framing plus lane cancellation; transport interfaces/sentinels. Small coherent ownership boundaries: a lone establishClient or cancellation helper split would mainly proliferate files. |

1. **Recommended smallest meaningful block: connection receive and handler dispatch.** Move exactly `(*Connection).readLoop`, `handlerLoop`, `callHandlerBounded` (including its complete explanatory comment), `callHandler` into one root `connection_receive.go`; new imports exactly `fmt`, `time`. Keep `MessageHandler`, `ConnectionConfig`, `queuedMessage`, `closeRequest`, `Connection`, all errors, `NewConnection` and shared `operationContext` in `connection.go`; it retains `context`, `errors`, `sync`, `sync/atomic`, `time` and loses only `fmt`. This removes a complete approximately 118-line runtime responsibility cluster, not one helper, and makes central definitions/constructor easier to browse. Reader control routing and application dispatch stay together; no channel, queueMu lease, ping/rekey/SessionClose routing, handler cancellation/recheck, bounded detachment or terminal ownership edits. Existing tests: `connection_test.go` (`TestConnectionPingPong`, `TestConnectionRemoteSessionClose`, `TestConnectionHandlerFailureContainment`, `TestConnectionSlowHandlerPreservesOrderAndBoundsQueue`, `TestConnectionShutdownCancelsBusyHandler`, both HandlerShutdownTimeout tests), `hardening_test.go` (`TestHardeningRunningHandlerQueueDisposal`, `TestHardeningStoppedQueueDisposal`), keepalive ACK/interleaving, start/close and send/shutdown properties. Baseline/post command: `go test -run 'Test.*(Connection|Handler|Queue|Shutdown|Close|Start|Keepalive|Ping|Rekey|Admission|Handoff|Abort)' -count=1 ./...`. Require parser equivalence, uncached full/default+keylog, vet/format/diff/staged review and supported-race/minimum-platform evidence or explicit deferral; one separately authorized commit.
2. **Larger alternative after the recommended block: session outbound/rekey cohesion.** Move exactly `(*Session).Send`, `SendPayload`, `sendPayload`, `MarkRekeySent`, `encryptLocked`, `rekeyDueLocked`, `startRekeyLocked`, and `sameRekeyFrame` to root `session_send.go` with imports `fmt`, `math`. Keep `Session`, `HandshakeSecrets`, `pendingSendRekey`, `ErrRekeyPending` and other errors, constructors/Secrets/SessionID/Close/Closed, all receive candidate/state logic and shared `validSessionMessageType`, `outboundMessage`, `newInboundMessage` in `session.go`. Original imports all remain needed (`math` by prepareRekeyLocked's receive-epoch exhaustion check, `fmt`/`encoding` by shared dispatch, `crypto/hmac` by receive confirmation). This is about 152 declaration/comment lines and separates an entire directional send transaction without moving public definitions or inventing epoch interfaces. Tests: `session_test.go` concurrent sequences/exhaustion/ownership; `session_nil_message_test.go`; `rekey_test.go`, `rekey_state_test.go`; `session_transport_edge_cases_test.go` epoch exhaustion/grace; `TestHardeningPendingRekeyOwned`; connection/send shutdown tests and tagged `TestKeyLogRecordsCommittedRekeyEpochs`. Baseline/post: `go test -run 'Test.*(Session|Rekey|Send|Queue|Shutdown|Close)' -count=1 ./...`, plus full/tagged/vet/format/parser/staged and supported race/platform gates. Do not split atomic receive/replay/rekey transitions into a new package.

Before either later block, reread full guides/checkpoint and freshly inspect branch/HEAD/status/full diffs, exact dependencies/tests and declaration comments. No additional source moves were performed during this reassessment. Discover the completed checkpoint commit via Git history rather than embedding its own hash.

## Historical block 2 preparation (superseded by landed checkpoints)

1. Re-read applicable `AGENTS.md` (including any newly added nested guides), its Git/security rules, this checkpoint, `connection.go`, callers, and root send/shutdown tests. Recheck `git branch --show-current`, `git status --short --branch`, `git --no-pager diff`, `git --no-pager diff --cached`, and `git log -1 -- docs/architecture/package-layout.md`; do not trust this session's clean state or test results.
2. Establish fresh focused baseline checks. In one subsequent commit, move only the cohesive send/queue method implementations: `Send`, `TrySend`, contextual/padded/write-wait variants, `enqueue`, `enqueueControl`, `enqueueChannel`, `writeLoop`, `writeMessage`, `complete`, and `failPending` into root `connection_send.go` with `package dgproto`. Keep `Connection`, configs, `queuedMessage`, `closeRequest`, sentinels and white-box tests at root. Keep lifecycle, handler, maintenance and read-loop implementations in place. Re-evaluate this exact method list against the current source before editing.
3. Preserve nonblocking versus contextual admission, post-enqueue cancellation (no retraction/snapshot), local-write completion only, control queue priority, serialized rekey/`MarkRekeySent` ordering, pending failure and first terminal cause. Do not redesign locking, channels, close handshakes, timers, handlers or buffer ownership. Add a focused regression before a fix only if an actual defect is separately authorized; this split has no intended behavior change.
4. Run `go test -run 'Test.*(Send|Queue|Shutdown|Close|Rekey)' -count=1 ./...` before and after; inspect selected test names to ensure coverage, including `send_api_test.go`, `send_shutdown_property_test.go`, `connection_start_close_test.go` and session/rekey edge tests. Then run default and `dgproto_keylog` tests, vet, formatting, diff checks, and supported race execution exactly as above. Diagnose/report limitations; do not weaken assertions.
5. Update this architecture checkpoint with actual completed scope and observed checks. Verify relative links, review full unstaged and staged diffs including new files, and stage only exact authorized paths. Preserve unrelated work; no bulk staging, reset/clean, release-note/candidate artifacts, push, tag or release. Commit only the authorized coherent block after passing code checks and explicitly report any environmental gate still unavailable; report commit hash and final status. Obtain authorization for any expanded scope.
