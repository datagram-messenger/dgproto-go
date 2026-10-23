# Repository Guide for Coding Agents and Contributors

This public guide applies throughout `dgproto-go`; read any more specific `AGENTS.md` in the directories you change. It describes a reusable engineering workflow, not permission to publish or a certification of production safety.

## Scope and sources of truth

`dgproto-go` implements the **draft DGProto v1 strict MVP** over TCP. It is a protocol/session library, not an application server. The protocol draft and Go module are versioned independently; passing tests alone does not establish stable wire compatibility or production assurance.

Use the relevant sources together, without treating implementation bugs as intended contracts:

- [README](README.md), [package documentation](doc.go), exported Go comments, callers, and tests: capabilities and public API behavior. [Getting started](docs/guides/getting-started.md) covers integration; [architecture](docs/architecture/overview.md) covers runtime structure.
- [Normative protocol](docs/protocol/dgproto-v1.md): current MVP wire format, cryptography, and state transitions, especially sections 3–5. Historical/post-MVP material is not an implemented feature or mandate to add one.
- [Vector policy](testdata/vectors/README.md), `testdata/vectors/`, `wire_vectors_test.go`, `conformance_test.go`, and `mvp_constraints_test.go`: deterministic fixtures and conformance probes. Parser acceptance is not semantic handshake/session validity or proof of cross-implementation interoperability.
- [go.mod](go.mod): module identity and minimum Go version. [CI](.github/workflows/ci.yml) and [release workflow](.github/workflows/release.yml): actual automated gates, not assumed coverage.
- [Contributing](.github/CONTRIBUTING.md): contribution workflow. [Security policy](SECURITY.md): private vulnerability reporting and application responsibilities.

If sources disagree, identify the conflicting behavior and evidence. Correct clear implementation defects within the task; ask before choosing a materially ambiguous contract or incompatible protocol change. Do not copy aspirational specification text into API guarantees (for example, there is no implemented `unsafe` fast path, automatic reconnect, or application server). `DialContext` is an additive required-pin TCP establishment helper returning a stopped runtime; it does not imply admission or application authorization.

## Repository orientation

| Area | Files and useful tests |
|---|---|
| Header, framing, TLV, typed messages | `header.go`, `frame.go`, `tlv.go`, `messages.go`; matching tests, `conformance_test.go`, `parser_fuzz_test.go` |
| Handshake and AEAD | `handshake.go`, `crypto.go`; handshake/crypto tests and vectors |
| Session, replay, directional rekey | `session.go`, `replay.go`, `rekey.go`; matching tests, `mvp_constraints_test.go` |
| Framed I/O and concurrent runtime | `transport.go`, `tcp.go`, `connection.go`, `server.go`; matching tests, `send_api_test.go`, `send_shutdown_property_test.go`, `connection_start_close_test.go`, `session_nil_message_test.go` |
| Debug traffic secrets | `keylog.go`, `keylog_disabled.go`, keylog tests; build-tag-specific behavior |
| Interoperability and performance | `wire_vectors_test.go`, `testdata/vectors/`, `benchmark_test.go` |

## Execute tasks with useful autonomy

1. Inspect current branch, `git status --short --branch`, and relevant diffs before editing. Read applicable instructions, affected code, exported contracts, normative sections, and representative tests. Do not rely on a previous session's branch or test results.
2. Classify the change: docs, bug, API, wire/crypto, lifecycle/resource bounds, dependencies, or performance. Pick a short implementation and validation plan proportional to its risk, then do the work. Routine in-scope edits, regression tests, and local checks do not need repeated permission requests.
3. Fix defects regression-first: reproduce with a focused test, observe failure when feasible, apply the fix, and rerun. If reproduction cannot safely run, explain why and what evidence supports the fix; never claim an unobserved failure.
4. Make the smallest **coherent** change, including necessary tests and contract documentation. Avoid speculative refactors, unrelated cleanup, new features, optimizations, dependencies, and fixture regeneration. Diagnose a failed check rather than weakening the invariant to pass it.
5. Ask only for material ambiguity, unsafe/destructive actions, or changes beyond the authorized scope (not every implementation detail). Escalate incompatible wire/crypto design decisions for explicit compatibility/security review.
6. Review the final diff and report what changed, exact checks and outcomes, limitations, and remaining gates. Finish useful authorized work rather than stopping at advice.

## Protocol, security, and test invariants

Read the normative sections before changing security-sensitive behavior; this checklist is not a replacement for the specification.

- Keep the MVP fixed to TCP, three-flight `Noise_XX_25519_ChaChaPoly_SHA256`, and ChaCha20-Poly1305. Do not infer negotiation, QUIC, alternate ciphers, resumption, 0-RTT, or obfuscation from historical names or parser-recognized identifiers.
- Preserve `DGP1`, version 1, the 40-byte header with little-endian numeric fields, no outer length prefix, the 65535-byte total frame limit, and the 16-byte data-frame tag. Handshake frames have zero session ID/sequence and no outer AEAD tag. Preserve Noise prologue, transcript-derived session ID, and directional Split key mapping as specified; do not add a second key derivation.
- Only `FlagPadding` is sendable; reserved sender bits/bytes remain zero. Receivers preserve unknown flags/reserved bytes and authenticate the exact received header. `Session` rejects type `0x07`; ignored inbound flag bits do **not** enable post-MVP semantics.
- Encrypted padding is cleartext random bytes of the requested length (0–255), authenticated with the header as AAD. Zero requested length means no padding, not zero-filled padding. Padding policy is application-owned, not transport obfuscation.
- Never reuse a key/nonce pair. Keep directional sequence allocation serialized and exhaustion explicit. Preserve rekey triggers and derivation labels in the specification and `rekey.go`, next-epoch-only transitions, constant-time confirmation, and bounded previous-key grace. Direct `Session.Send`/`SendPayload` callers must write an emitted `RekeyInit`, call `MarkRekeySent`, then retry the original message; `Connection` performs that ordering itself.
- Failed authentication must not advance replay state. Rekey validation and key/epoch/replay/grace changes commit atomically. An authenticated malformed ordinary message **does** consume its sequence; do not change that contract accidentally.
- Validate lengths and integer arithmetic before peer-sized allocations; keep frame/TLV/message parsing bounded, panic-free on hostile input, and faithful to documented ownership. Test truncation, maximum/over-limit inputs, unknown/reserved fields, invalid order, mutation/aliasing, authentication failures, and replay/rekey boundaries.
- Preserve documented concurrency: `Session` and `Connection` exported methods are concurrent-safe; `TCPTransport` serializes reads with reads and writes with writes, allowing one read and one write concurrently. Do not assume standalone handshake/replay helpers or caller-owned message buffers are safe for concurrent mutation.
- Use deterministic, bounded tests with synchronization instead of timing guesses. Cover cancellation (including pre-canceled calls), typed-nil messages, partial I/O, queue saturation, concurrent send/rekey/close, close-before-start, slot release, hook errors/panics, and stable first terminal cause where affected.
- Preserve existing vectors; add targeted valid/invalid cases and minimized public-safe fuzz regressions when needed. Do not regenerate unrelated fixtures or require every receive-valid header to be sendable. Check byte-for-byte round trips at the validation level specified by each fixture.

## Production integration: guarantees and limits

Changes and examples must retain these distinctions. Deployment policy needs application-specific evidence, not universal timeout recipes.

- **Identity and admission:** Noise proves possession of a static key, not account identity or permission. An empty `AllowedClients` permits any valid authenticated client. Use allowlisting and/or `Admission` before dispatch for authorization; clients must verify the intended server identity. Persist/protect provisioned static keys and plan rotation. Do not use fresh demo keys as a production identity strategy.
- **Resource budgets:** `NewServer` defaults to queues of 16, 64 concurrent handshakes, and 1024 active connections. Saturated admission slots reject connections, not an unlimited wait queue. Preserve independent limits and slot release on failure/shutdown. Applications must also bound handler payloads, downstream work, retries, and per-peer abuse; library counts are not comprehensive DoS protection.
- **Timeouts:** zero server `HandshakeTimeout` selects 10 seconds. Read/write/idle timeouts and keepalive interval are disabled by default. Enabled keepalive with zero timeout selects twice the interval. `NewServer` permits zero `ReadTimeout`; when positive, it must cover the configured idle/keepalive liveness window (including `IdleTimeout`). Derive explicit settings from service budgets and test slow/stalled peers; do not assume direct `NewConnection` applies all server validation.
- **Backpressure and delivery:** `Send`/`TrySend`/`SendPadded` are nonblocking queue acceptance, returning `ErrOutboundQueueFull` on saturation. `SendContext` waits for capacity; `SendAndWait` additionally waits for the local write, not peer receipt, processing, or acknowledgement. Cancellation after enqueue is not guaranteed to retract a message. Keep application retries/idempotency explicit and bounded; do not silently drop or infinitely buffer work.
- **Handlers:** messages dispatch serially per connection; slow handlers delay later messages, and a full handler queue terminates that connection. Handler errors/panics terminate it. Handlers must honor cancellation and bound downstream calls; do not promise parallel stream processing.
- **Shutdown:** `Connection.Close` attempts a close-frame write then stops transport; it is not peer acknowledgement or a queue-drain guarantee. `Done` tracks runtime-loop completion, and `Err` retains the first terminal cause. `Server.Close` waits for server-owned work. Positive `HandlerShutdownTimeout` bounds the post-cancellation wait for a message handler by detaching it, not killing it; zero can leave completion blocked indefinitely. This is not an overall server shutdown deadline.
- **Hooks:** `Admission` is synchronous before runtime start and receives a fresh handshake-timeout context; a callback ignoring it is not forcibly interrupted. Admission errors/panics reject the connection. `OnDisconnect` runs once for a connection that passed admission, with a background context; panics are recovered, but no built-in hook deadline exists. Bound hook I/O yourself. `Server.Abort` closes networking without waiting for handlers/hooks; an external shutdown coordinator must enforce the service deadline and escalation. Do not imply abort kills callback goroutines or guarantees `Close` subsequently returns.
- **Operational safety:** keep logs/metrics free of payloads and secrets; review identity metadata for privacy. Observe admission rejection, queue pressure, timeouts, terminal causes, and downstream latency in application instrumentation. Test load, stalled peers, disconnect storms, and shutdown against measured memory/goroutine/latency budgets before rollout.

## Go API, compatibility, and dependencies

- Target `go.mod`'s minimum Go version; write idiomatic, `gofmt`-clean Go. Preserve exported names/interfaces, defaults, ownership, blocking/cancellation, error sentinels, and `errors.Is`/`errors.As` behavior. Document contracts and non-obvious invariants, not obvious code.
- No peer-input panics, unbounded queues/goroutines/retries, or undocumented ownership changes. Distinguish documented programmer-misuse panics from hostile-input handling.
- Public behavior changes require deliberate scope, tests, and synchronized exported/user docs. Breaking API changes need a versioning/migration decision. Wire changes need explicit draft/version compatibility decisions and coordinated specification, implementation, vectors, and interoperability/security review. A bug fix restoring the existing spec does not require inventing a new wire rule.
- Prefer the standard library and existing dependencies. For necessary additions/updates, review purpose, provenance, license, minimum-Go impact, transitive/module diffs, advisories, and maintenance risk; do not upgrade speculatively. Crypto changes, `unsafe`, pooling, or new algorithms need demonstrated benefit and explicit design/security review.
- `govulncheck` detects known vulnerability/reachability information, not unknown flaws, authorization mistakes, protocol correctness, or deployment safety. Record scanner/toolchain versions and investigate findings; a clean scan is not an audit.

## Validation by change class

Run from the repository root. Commands below use portable Go/Git syntax; replace test/filename placeholders with actual affected names. Record `go version` and `go env GOOS GOARCH CGO_ENABLED` for code-check reports.

For every code change, format changed Go files with `gofmt -w` followed by their filenames, run focused tests during iteration, then the baseline:

```sh
gofmt -l .
go test -count=1 ./...
go vet ./...
git diff --check
```

Formatting output must be empty. Add the applicable gates (they do not replace the baseline):

| Change class | Validation |
|---|---|
| Documentation only | Check relative links/anchors, balanced language-tagged fences, facts against code/spec/CI, `git diff --check`, and final status/diff. Compile new/changed Go examples in a temporary consumer module using a local `replace` to this checkout; `doc.go` also needs `gofmt`. Full code tests need not rerun for prose-only changes; say so. |
| Wire, parser, messages, crypto | `go test -run TestJSONWireVectors -count=1 ./...`, plus relevant conformance/MVP tests (prefixes `TestWIRE`, `TestHS`, `TestSESS`, `TestP0`) and affected codec/handshake/session tests selected with `-run`. Check semantic validity, not just parser round trips. |
| Parser/framing fuzzing | Run one affected target at a time, e.g. `go test -run "^$" -fuzz "^FuzzFrameUnmarshalBinary$" -fuzztime=60s -parallel=2`. Other existing targets: `FuzzHeaderUnmarshalBinary`, `FuzzDecodeTLVs`, `FuzzMessageUnmarshalBinary`, `FuzzTCPTransportReadFrame`. Report target, duration, resource limits, and findings; seed tests alone are not a fuzz campaign. |
| Concurrency, transport, lifecycle, admission/rekey | Focused boundary/property tests, then `go test -race -count=1 ./...` on a supported host with CGO and a C toolchain. Include Linux race and actual target-platform tests for OS-sensitive behavior. |
| Keylog implementation/build tags | `go test -tags dgproto_keylog -count=1 ./...` as well as default-build tests; keep secret output confined to synthetic test fixtures. |
| Dependencies/module/toolchain | `go mod verify`, `govulncheck ./...`, baseline, and tidy-diff check below. Use the scanner version selected by CI; installation changes the local tool environment and needs available network/toolchain. |
| Performance-sensitive | `go test -run "^$" -bench . -benchmem -benchtime=5s`; compare before/after with the same toolchain, hardware, workload, and parameters. Benchmark numbers are not service load/SLO evidence. |

CI also enforces module tidiness:

```sh
go mod tidy
git diff --exit-code -- go.mod go.sum
```

`tidy` can modify files or download modules. Check from a clean module baseline or a disposable worktree when unrelated module edits exist. For intentional dependency changes, review the expected diff first and verify tidiness against that new baseline; do not erase legitimate edits to obtain a zero diff.

Current CI runs Go 1.25.x ordinary tests on Linux, Windows, and macOS; Linux additionally runs race, keylog-tag tests, and `govulncheck`, with formatting/vet/tidy checks in the quality job. Fuzz campaigns, benchmarks, independent interoperability, and deployment load tests are **not** automated CI gates. Newer Go or cross-compilation does not replace testing the minimum version or executing tests on the target OS. If race/CGO or a platform is unavailable locally, report the gap and require the applicable CI/host evidence before merge; do not label it passed.

## Public safety, Git hygiene, and release gates

- Report suspected vulnerabilities privately via [SECURITY.md](SECURITY.md), not public issues/PRs/commits. Keep exploit details and private investigation notes out of public artifacts; use synthetic, non-sensitive reproductions through the agreed disclosure process.
- Never publish private/static traffic keys, credentials, key logs, production captures, personal data, private notes, or machine/account/session metadata. Deterministic test-only keys are not production credentials. Inspect staged content as well as filenames; ignored files are not source material for public docs.
- Key logging requires **both** the `dgproto_keylog` build tag and `DGPROTO_KEYLOG`. Normal builds are no-ops. Keep the tag out of production builds and the variable unset in normal tests/examples/deployments. Dedicated keylog tests may set it for synthetic data in temporary files; do not disable those tests with a blanket prohibition. Protect/delete debugging output; do not promise file-mode protection is sufficient on every OS or that existing files are secured automatically.
- Preserve unrelated tracked/untracked work. Do not reset, clean, force-push, rebase shared history, or bulk-stage artifacts to finish a task. Review `git diff`, `git diff --check`, and `git status --short`; stage only authorized change files when staging is requested.
- Commit, push, create/update PRs, tag, or publish releases **only when authorized**. File-edit permission alone does not authorize these actions. When requested, keep commits coherent and summarize motivation, behavioral impact, compatibility, security implications, and exact validation in the PR without secrets.
- Release automation validates an exact existing stable SemVer tag, runs Linux tests/race/vet/format/tidy/vulnerability checks, and an external-consumer **import smoke test** using a local replacement. It does not prove published-module availability or full consumer API coverage. Tag pushes can publish automatically; manual dispatch can validate only or publish. Treat both as maintainer-authorized actions; do not add unsupported claims about signing or publication checks.
- Before release, maintainers must confirm current CI/platform/keylog results, relevant fuzz/vector/interoperability evidence, API/wire compatibility and migration notes, dependency review, and service-specific security/load/shutdown gates. Passing automation or improving this guide is not library production certification.

## Definition of done

- The requested coherent scope is complete; affected code, tests, fixtures, exported docs, and normative contracts agree, or an unresolved conflict is explicitly reported.
- A bug has a focused regression; applicable baseline and risk-specific checks pass. Documentation-only work has its own checks instead of fictitious code-test results.
- Report exact commands, toolchain/platform, observed pass/fail/skip outcomes, and reasons/remaining gates. Distinguish current local evidence from identified CI evidence, cached results, and checks not rerun.
- Formatting/module checks apply to relevant changes; `git diff --check` passes. Final/staged diffs contain no secrets, artifacts, unrelated edits, or unexplained compatibility changes; report changed paths and working-tree status.
- No commits or remote mutations occurred without authorization. State deployment risks and remaining production evidence honestly rather than declaring the library production-ready.
