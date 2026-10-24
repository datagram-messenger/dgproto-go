# Incremental package layout

This is an architecture plan, not a release checklist or a compatibility certification. Read the [runtime overview](overview.md), [repository guide](../../AGENTS.md), [public package overview](../../doc.go), and [protocol specification](../protocol/dgproto-v1.md) together.

## Boundaries and staged roadmap

The root `dgproto` package remains the public API and owns its exported definitions. File size alone does not justify a new package. Prefer cohesive root files before inventing interfaces or moving state across ownership boundaries.

| Block | Scope | Gate to proceed |
|---|---|---|
| 1 (landed) | Extract the stateless accept-error classifier to `internal/acceptpolicy`; retain the root wrapper and runtime tests. | Direct/wrapped regression parity and default/tagged package checks. |
| 2 (landed, separate commit) | Root-only connection send/queue implementation split into `connection_send.go`; types and white-box tests stay at root. | Preserve enqueue, cancellation, writer/rekey ordering, completion and shutdown contracts. |
| 3 (future, separate commit) | Root-only keepalive/maintenance split into `connection_keepalive.go`. | Preserve timer reset, ping acknowledgement, idle timeout and cancellation behavior with focused liveness tests and race execution. |
| 4 (future, separate commit) | Root-only lifecycle split into `connection_lifecycle.go`. | Preserve start/close/abort ownership, first cause, handler completion and server wait boundaries. |
| 5 (reassessment, not promised extraction) | Reconsider real internal seams only after root cohesion improves. | Prove a one-way dependency graph and explicit state/buffer ownership; reject alias facades and speculative abstractions. |

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

Next executable block: move only `defaultKeepaliveTimeout`, `beginPing`, `clearPing`, `acknowledgePing`, `consumePingAcknowledgement`, `maintenanceLoop`, `noteActivity`, `resetTimer` into root `connection_keepalive.go`, preserving source bodies/comments and timer/state/lock ownership. Keep reader and handler in `connection.go`. Establish fresh `Test.*(Keepalive|Ping|Idle|Send|Queue|Shutdown|Close|Rekey)` baseline, then repeat focused/default/tagged/vet/format/diff/equivalence gates and a separate local commit. Block 4 follows only after that commit and clean-status recheck. Discover actual checkpoint hashes using Git history, not an embedded self-hash.

## Historical block 2 preparation (superseded by landed checkpoint)

1. Re-read applicable `AGENTS.md` (including any newly added nested guides), its Git/security rules, this checkpoint, `connection.go`, callers, and root send/shutdown tests. Recheck `git branch --show-current`, `git status --short --branch`, `git --no-pager diff`, `git --no-pager diff --cached`, and `git log -1 -- docs/architecture/package-layout.md`; do not trust this session's clean state or test results.
2. Establish fresh focused baseline checks. In one subsequent commit, move only the cohesive send/queue method implementations: `Send`, `TrySend`, contextual/padded/write-wait variants, `enqueue`, `enqueueControl`, `enqueueChannel`, `writeLoop`, `writeMessage`, `complete`, and `failPending` into root `connection_send.go` with `package dgproto`. Keep `Connection`, configs, `queuedMessage`, `closeRequest`, sentinels and white-box tests at root. Keep lifecycle, handler, maintenance and read-loop implementations in place. Re-evaluate this exact method list against the current source before editing.
3. Preserve nonblocking versus contextual admission, post-enqueue cancellation (no retraction/snapshot), local-write completion only, control queue priority, serialized rekey/`MarkRekeySent` ordering, pending failure and first terminal cause. Do not redesign locking, channels, close handshakes, timers, handlers or buffer ownership. Add a focused regression before a fix only if an actual defect is separately authorized; this split has no intended behavior change.
4. Run `go test -run 'Test.*(Send|Queue|Shutdown|Close|Rekey)' -count=1 ./...` before and after; inspect selected test names to ensure coverage, including `send_api_test.go`, `send_shutdown_property_test.go`, `connection_start_close_test.go` and session/rekey edge tests. Then run default and `dgproto_keylog` tests, vet, formatting, diff checks, and supported race execution exactly as above. Diagnose/report limitations; do not weaken assertions.
5. Update this architecture checkpoint with actual completed scope and observed checks. Verify relative links, review full unstaged and staged diffs including new files, and stage only exact authorized paths. Preserve unrelated work; no bulk staging, reset/clean, release-note/candidate artifacts, push, tag or release. Commit only the authorized coherent block after passing code checks and explicitly report any environmental gate still unavailable; report commit hash and final status. Obtain authorization for any expanded scope.
