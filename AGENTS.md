# Repository guide for agents and contributors

Applies throughout `dgproto-go`; read any more specific `AGENTS.md` for the paths you change. This is a working guide for humans and coding agents, not permission to publish or a production-safety certification.

## Sources of truth and scope

This library implements the **draft DGProto v1 strict MVP over TCP**, not an application server. Protocol draft and Go module versions are independent; neither the `v1` name nor passing tests guarantees stable wire compatibility.

- [Protocol specification](docs/protocol/dgproto-v1.md) is normative for wire behavior, especially sections 2–5. Historical/post-MVP material is not implemented functionality or a task to add it.
- [README](README.md), [package overview](doc.go), exported comments, implementation, callers, and tests establish the public API and current behavior. [Getting started](docs/guides/getting-started.md) covers integration; [architecture](docs/architecture/overview.md) explains the runtime.
- [Vector policy](testdata/vectors/README.md) defines fixture scope. Parser acceptance is not semantic handshake/session validity or independent interoperability evidence.
- [go.mod](go.mod) defines module identity, dependencies, and minimum Go version (currently 1.25.0). Use a security-patched toolchain on that line; consult README security guidance rather than freezing patch/advisory lists here.
- [Contribution policy](.github/CONTRIBUTING.md) and [security policy](SECURITY.md) govern contributions and private vulnerability reporting. [CI](.github/workflows/ci.yml) and [release workflow](.github/workflows/release.yml) define actual automated gates; broad policy lists are not proof those gates exist or passed.

When sources conflict, identify the exact discrepancy and evidence. Do not treat a bug as a contract or promote design aspirations into guarantees. Fix clear defects within scope; ask before resolving material ambiguity or changing an incompatible API/wire contract.

## Repository map

| Area | Implementation and starting tests |
|---|---|
| Headers, frames, TLV, typed messages | `header.go`, `frame.go`, `tlv.go` (public concrete TLV, copies/current sentinels), `internal/tlv/codec.go` (bounded arithmetic, two-pass byte codec), `messages.go` (shared sizes/errors, application/control families), `messages_handshake.go` (three-flight wrappers and NoisePattern); `internal/tlv/codec_test.go` (algorithm/golden/alignment), `tlv_api_test.go` (external identity/error/ownership/allocation), `tlv_decode_limits_test.go`, `tlv_encode_regression_test.go`, matching tests, `messages_handshake_test.go`, `conformance_test.go`, `parser_fuzz_test.go` |
| Noise handshake and AEAD | `static_key.go` (root static identity/loading/copy/validation), `handshake.go` (whole flight state machine), `crypto.go`, `server_handshake.go` (responder establishment, allowlist and root frame validator shared with client); matching tests, `server_test.go`, `client_*_test.go`, `hardening_test.go`, `mvp_constraints_test.go` |
| Session, replay, directional rekey | `session.go` (central state/construction/close and shared dispatch), `session_send.go` (outbound/rekey transaction), `session_receive.go` (atomic candidate/authentication/replay/rekey/grace transaction), `replay.go` (public value-storage/token/error facade), `internal/replay/window.go` (validation/commit/bitmap algorithm, colocated `window_test.go`), `rekey.go`; `replay_api_test.go` and matching tests, `rekey_state_test.go`, `session_transport_edge_cases_test.go` |
| TCP establishment and I/O | `client.go`, `transport.go`, `tcp.go`; root `client_*_test.go`, `tcp_test.go`; public-only rejection/EOF/cancellation scenarios in `integrationtests/tcp_test.go` |
| Runtime, queues, admission, shutdown | `connection.go` (definitions, constructor, shared operation context), `connection_receive.go` (reader/control routing and serial/bounded handler), `connection_send.go` (send/queue/writer), `connection_keepalive.go` (maintenance/timers/ping ACK), `connection_lifecycle.go` (start/close/terminal ownership), `server.go` (definitions/defaults, accept loop and shutdown), `server_runtime.go` (admission/disconnect, worker/slot ownership and atomic handoff); `server_runtime_test.go` (both serialized shutdown snapshots), matching tests, `send_api_test.go`, `send_shutdown_property_test.go`, `connection_start_close_test.go` |
| Optional traffic-secret logging | `keylog.go`, `keylog_disabled.go`, keylog tests |
| Deterministic fixtures and performance | `testdata/vectors/`, `wire_vectors_test.go`, `benchmark_test.go`, TLV benchmark tests |

Root imports `internal/wire`, `internal/tlv`, `internal/replay` and `internal/acceptpolicy`; leaves import only the standard library, never root. `internal/wire/header.go` owns the complete primitive fixed-header codec and shape constants; root `header.go` retains concrete identity, current Magic/sentinels and all semantic validation. Start fixed-header work with `internal/wire/header_test.go`, external `header_api_test.go`, exact-octet/AAD `header_wire_test.go`, existing header/frame/crypto/conformance/vector tests and bounded-worker header/frame fuzz campaigns. PutHeader/ReadHeader require at least 40 bytes; transient value conversions allocate nothing but have measured CPU overhead, so compare `BenchmarkHeaderBoundary` before changing that boundary. TLV algorithm tests belong beside `internal/tlv/codec.go`; root/public tests retain concrete identity, mutable sentinel wrapping, copy/nil/empty and allocation contracts. Use byte/scalar Read/Write only after their documented full validation/allocation preconditions; do not add an intermediate descriptor conversion or split session/runtime atomic ownership. Start with `go test -count=1 ./internal/tlv` and root TLV/vector/conformance tests, then full default/tagged gates and a bounded-worker `FuzzDecodeTLVs` campaign for parser changes. Compare affected benchmarks before accepting boundary overhead. `internal/wire/control.go` owns whole PingPong/Ack/RekeyInit/post-MVP ticket primitive layouts; root `messages.go` retains concrete definitions, validation/current sentinels and owned allocation. Primitive buffers/counts must be fully validated; ReadTicket returns only a transient borrowed view that root copies once. Algorithm/golden/precondition tests belong in `internal/wire/control_test.go`, public identity/error/nil/ownership/allocation regressions and matched `BenchmarkControlMessageBoundary` in `messages_api_test.go`. Run focused control/vector/conformance tests and a two-worker `FuzzMessageUnmarshalBinary` campaign for changes. Rekey confirmation/epoch policy and Session atomic state remain root; ticket byte coding is not MVP resumption support.

## Working workflow

1. Inspect applicable instructions, current branch, `git status --short --branch`, unstaged **and staged** diffs before editing. Identify existing work and affected contracts/tests; do not trust a previous session's state or results.
2. Choose a short plan and checks proportional to risk: prose, bug/API, wire/crypto, concurrency/resources, dependencies, or performance. Proceed with routine authorized edits and local checks without repeated permission requests; ask for material ambiguity, destructive actions, or scope expansion.
3. For defects, add a focused regression, observe failure when feasible, fix, and rerun. If reproduction is unsafe or unavailable, state the evidence and limitation; do not claim an unobserved failure.
4. Make the smallest coherent change, including necessary tests and contract docs **within authorized scope**. Preserve exported names, defaults, ownership, blocking/cancellation, error sentinels, and `errors.Is`/`errors.As` behavior. Breaking API changes need a migration/version decision; wire changes need coordinated spec, implementation, vectors, and compatibility/security review.
5. Avoid unrelated cleanup, speculative features/refactors, dependency upgrades, fixture regeneration, or optimizations. Prefer existing dependencies and the standard library; review necessary dependency changes for provenance, license, advisories, transitive and minimum-Go impact. Crypto, `unsafe`, pooling, or algorithm changes need evidence and explicit design/security review.
6. Diagnose failed checks rather than weakening invariants. Report out-of-scope defects without silently fixing them. Review final diffs/status and finish useful authorized work, not just a plan.

## Contracts to preserve

These are implemented behavior to verify in affected code/tests, not a substitute for reading the specification.

### Wire, crypto, and receive state

- TCP only; three-flight `Noise_XX_25519_ChaChaPoly_SHA256` and ChaCha20-Poly1305. No cipher negotiation, QUIC, resumption, 0-RTT, obfuscation, or automatic reconnect. Recognized reserved identifiers do not implement their semantics; `Session` rejects message type `0x07`.
- Preserve `DGP1`, version 1, the 40-byte little-endian header, no outer length prefix, 65535-byte **total** frame limit, and 16-byte data-frame tag. Handshake session ID/sequence are zero and have no outer AEAD tag. Preserve the `DGPv1` Noise prologue, transcript-derived session ID, and role-oriented Split keys; do not add another key derivation.
- Sender flags allow only `FlagPadding`; reserved sender bits/bytes are zero. Receivers preserve unknown flags/reserved bytes and authenticate the exact received header. Data-frame padding is random **cleartext**, 0–255 bytes, included with the header in AEAD AAD; zero length means no padding. This is distinct from TLV alignment padding and is not obfuscation.
- Never reuse a key/nonce pair. Serialize directional sequence allocation and make exhaustion explicit. Preserve rekey triggers/labels, next-epoch-only transitions, constant-time confirmation, and bounded previous-key grace in `rekey.go`/`session.go`. Direct `Session.Send`/`SendPayload` users must write an emitted `RekeyInit`, call `MarkRekeySent`, then retry the original message; `Connection` handles that ordering.
- Authentication failure must not advance replay state. Rekey validation and epoch/key/replay/grace changes commit atomically. An authenticated malformed **ordinary** message consumes its sequence; invalid rekey must not partially commit state.
- Check lengths/overflow before peer-sized allocation. Preserve bounded, hostile-input-safe parsers and documented copy/alias behavior; no new peer-input panics or unbounded queues/goroutines/retries. Documented programmer-misuse panics are a separate contract.

### Identity, ownership, concurrency, and lifecycle

- Noise authenticates static-key possession, not an account or permission. Empty `AllowedClients` allows any valid authenticated client; authorization belongs in allowlisting and/or `Admission`. Applications own trusted key/pin provisioning, persistence, rotation, and secret storage.
- `DialContext` requires a server pin, bounds dial plus handshake (zero timeout selects 10 seconds), and returns a **stopped, caller-owned** connection. Call `Start(runtimeCtx)` and always close it, even if unstarted. Establishment cancellation after success does not stop its runtime; success means a local final-flight write, not server admission/readiness or application authorization. Lower-level handshake helpers do not impose the client's mandatory-pin policy.
- `Session` and `Connection` exported methods are concurrent-safe; `TCPTransport` serializes reads with reads and writes with writes, permitting one read and one write concurrently. This does not make standalone handshake/replay state or caller-owned buffers safe for concurrent mutation.
- `Send`/`TrySend`/`SendPadded` accept into a bounded queue without waiting (`ErrOutboundQueueFull` on saturation). Context variants wait for capacity; write-wait variants additionally wait for the complete **local** write, including required rekey. None promises peer receipt, processing, acknowledgement, or durable delivery.
- Enqueue does **not** snapshot messages or reachable buffers: keep them immutable while pending. Successful write-wait permits reuse; cancellation after enqueue does not retract the message and may return before serialization finishes, so cancellation alone does not permit reuse. Use fresh immutable messages for acceptance-only sends; applications own bounded retries/idempotency.
- Handlers run serially per connection. Slow handlers delay later messages; queue overflow, handler error, or panic terminates the connection. Stream IDs do not imply parallel dispatch.
- Server defaults: queues of 16, 64 concurrent handshakes, 1024 active connections, 10-second handshake timeout. Saturated slots reject connections; preserve independent limits and release on failure/shutdown. Read/write/idle timeouts and keepalive are disabled by default. Enabled keepalive with zero timeout selects twice its interval. Server/client configuration validation allows zero read timeout; when positive, it must cover the idle/keepalive window. Direct `NewConnection` does not apply all those checks. Defaults are not comprehensive DoS protection: derive and test service budgets.
- `Connection.Close` attempts a close-frame write, not a queue drain or peer-acknowledged close. `Done` tracks runtime completion; `Err` retains the first observed terminal cause. `Server.Close` waits for server-owned work. Positive `HandlerShutdownTimeout` detaches a stuck canceled handler, not kills it; zero can wait indefinitely. It is not an overall shutdown deadline.
- `Admission` runs synchronously before runtime start with a fresh handshake-timeout context; errors/panics reject that connection. `OnDisconnect` runs once for an admitted connection with a background context; panics are recovered but no hook deadline is imposed. `Abort` closes networking without waiting for callbacks; it cannot kill goroutines or guarantee a later `Close` returns. Bound callback/downstream work and coordinate service-level shutdown externally.

## Validation proportional to risk

Run commands from the repository root in a compatible shell. Replace filename/test selectors with actual affected names; Go/Git commands below do not require Bash. Record `go version` and `go env GOOS GOARCH CGO_ENABLED` for code-check reports.

For code changes, `gofmt -w` only changed Go files, iterate with focused regression tests, then run:

```sh
gofmt -l .
go test -count=1 ./...
go vet ./...
git diff --check
```

Formatting output must be empty. Add relevant gates; do not indiscriminately run every expensive check for every edit.

| Change/risk | Additional validation |
|---|---|
| Prose only | Check relative links/anchors, balanced language-tagged fences, filenames, constants/defaults and claims against code/spec/workflows; inspect final diff/status and run `git diff --check`. No full code-test rerun required; report that explicitly. Compile new/changed Go examples in a temporary consumer module with a local `replace`; format changed `doc.go`. |
| Wire/parser/message/crypto | `go test -run TestJSONWireVectors -count=1 ./...`, affected codec/handshake/session tests, and relevant conformance/MVP selectors (`TestWIRE`, `TestHS`, `TestSESS`, `TestP0`). Preserve deterministic fixtures and their stated validation level; parser round trips alone are insufficient. Add targeted cases, not unrelated fixture regeneration. |
| Hostile-input parsing/framing | Test truncation, max/over-limit lengths, overflow, unknown/reserved fields, TLV alignment, mutation/aliasing, and panic resistance. Fuzz one affected target at a time, e.g. `go test -run "^$" -fuzz "^FuzzFrameUnmarshalBinary$" -fuzztime=60s -parallel=2`. Other targets are in `parser_fuzz_test.go`; retain minimized public-safe regressions. Record duration/resources; running seeds is not a fuzz campaign. |
| Concurrency/I/O/lifecycle/rekey | Focused boundary/property tests, then `go test -race -count=1 ./...` with supported CGO/C toolchain. Cover affected cancellation, typed-nil, partial-I/O, saturation, send/rekey/close, close-before-start, slot release, callback failures, and first-cause boundaries. Use synchronization and bounded waits, not timing guesses. OS-sensitive changes also need target-OS execution. |
| Keylog/build tags | Default tests plus `go test -tags dgproto_keylog -count=1 ./...`; synthetic secrets only in temporary files. |
| Dependencies/module/toolchain | Baseline, `go mod verify`, `govulncheck ./...` using CI's scanner version, and tidy check below. Review module diffs/advisories. A clean vulnerability scan is not a crypto, authorization, or deployment audit. |
| Performance | `go test -run "^$" -bench . -benchmem -benchtime=5s`; compare before/after on the same toolchain, hardware, workload, and parameters. Benchmarks do not establish service SLOs. |

CI's module check is:

```sh
go mod tidy
git diff --exit-code -- go.mod go.sum
```

`tidy` may download modules or modify files. Use a clean module baseline or an isolated copy/worktree when unrelated module edits exist; for intentional dependency changes, verify against the reviewed new baseline. Never erase legitimate edits to obtain a clean check.

Current CI uses Go 1.25.x: ordinary tests on Linux, Windows, macOS; Linux race, keylog-tag tests, vulnerability scan, and formatting/vet/tidy checks. Fuzz campaigns, benchmarks, independent interoperability, signing, and deployment/load checks are **not** automated gates in these workflows. Read workflows again when changing/releasing; configured checks are not observed successful runs. Newer-Go tests or cross-compilation do not replace minimum-line and target-OS execution. Report unavailable checks and required CI/host evidence, not fictional passes.

## Security, Git, and release safety

- Follow [SECURITY.md](SECURITY.md) for private reports; keep exploit details/private investigation out of public issues, PRs, commits, and fixtures. Use synthetic, public-safe regressions through the disclosure process.
- Never publish private keys, credentials, traffic keylogs, production captures, personal data, or private machine/account/session notes. Deterministic test-only keys are not production credentials. Inspect content, including staged content, not just filenames. Keep logs/metrics free of secrets and unnecessary identity/payload data.
- Traffic-secret logging requires **both** `dgproto_keylog` and `DGPROTO_KEYLOG`; default builds are no-ops. Keep the tag out of production and the variable unset for ordinary runs. Dedicated tests may set it for synthetic temporary data. Protect/delete output; file modes do not guarantee cross-OS protection or secure pre-existing files.
- Preserve unrelated tracked/untracked work. Do not reset, clean, force-push, rewrite shared history, or bulk-stage to finish a task. Stage only authorized paths when staging is requested. Commit, push, PR mutations, tagging, workflow dispatch, and release publication require authorization; editing alone grants none.
- During explicitly authorized commit/push work, including the current ongoing internal-packages refactor, commit each tested, reviewed coherent step promptly rather than accumulating many unrelated files for a final commit. Stage explicit paths, inspect the complete staged diff and run `git diff --cached --check` before each commit. Separate independent implementation, tests, documentation, and pure renames when sensible; keep coupled implementation and regression tests together rather than splitting solely by file count. Push only within the user's explicit authorized scope and use a non-force push to the intended remote/branch. Authorization for this session is not perpetual remote authorization for future unrelated requests. The security and release-artifact restrictions below still apply.
- **Do not create, track, stage, commit, or push release-note/release-candidate working files under `docs/`**, such as `docs/*-release-notes.md` or `docs/*-candidate.md`. Useful local notes may remain **untracked outside `docs/`**; verify they are absent from staged changes before any commit/push. Public tracked release documentation requires an explicit request for that exact file. Do not repair a missing release-doc link by creating a forbidden artifact.
- Release automation checks an exact existing stable `vMAJOR.MINOR.PATCH` tag, runs Linux tests/race/vet/format/tidy/vulnerability checks, and an external-consumer **import smoke test with a local replacement**. This does not verify published-module availability or full consumer API coverage. Tag pushes can publish automatically; manual dispatch validates and optionally publishes. Both require maintainer authorization. Release validation does not itself run keylog-tag or other-platform tests.
- Before an authorized release, confirm current CI/platform/keylog results, API/wire compatibility, relevant vector/fuzz/interoperability evidence, dependency review, and service-specific security/load/shutdown budgets. Describe migration implications without creating unrequested tracked release files. Passing automation is not production certification.

## Completion report

Report changed paths and behavioral/compatibility impact; exact commands with observed pass/fail/skip outcomes; toolchain/platform where relevant; unresolved discrepancies and remaining gates. Distinguish local results, cached evidence, configured CI, and checks not run. Review `git diff`, staged diff, `git diff --check`, and `git status --short --branch` for artifacts, secrets, or unrelated edits. State Git status and whether any authorized commit/remote action occurred. Do not claim interoperability or deployment readiness without that evidence.
