# Incremental package layout

This is an architecture plan, not a release checklist or a compatibility certification. Read the [runtime overview](overview.md), [repository guide](../../AGENTS.md), [public package overview](../../doc.go), and [protocol specification](../protocol/dgproto-v1.md) together.

The authoritative resume point is **Current checkpoint: block 8 landed, session receive and final reassessment** below. All older checkpoints and proposed scopes are historical evidence, not instructions to repeat landed work.

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

## Current checkpoint: block 8 landed, session receive and final reassessment

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
