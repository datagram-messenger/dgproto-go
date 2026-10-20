# Repository Guide for Coding Agents

Welcome to `dgproto-go`. This document gives coding agents and contributors the project-specific context needed to make safe, reviewable changes. It applies to the entire repository unless a more specific `AGENTS.md` exists in a subdirectory.

## Project overview

`dgproto-go` is the Go 1.25+ implementation of the strict DGProto v1 MVP wire protocol and secure session runtime. It is a protocol library, not the application server.

Start with [`README.md`](README.md) for capabilities and usage, and follow [`.github/CONTRIBUTING.md`](.github/CONTRIBUTING.md) for the contribution and pull request workflow. For wire-visible behavior, [`docs/protocol/dgproto-v1.md`](docs/protocol/dgproto-v1.md) is normative. Read [`SECURITY.md`](SECURITY.md) before reporting or handling a suspected vulnerability.

## Sources of truth

Use the narrowest authoritative source for each decision:

1. `docs/protocol/dgproto-v1.md` defines wire format, cryptographic derivations, state transitions, limits, and interoperability requirements.
2. Exported Go documentation and existing public behavior define the library API contract.
3. `testdata/vectors/` and `wire_vectors_test.go` define deterministic interoperability fixtures; the vectors are immutable unless an intentional protocol revision requires coordinated replacement.
4. `.github/workflows/ci.yml` and `.github/workflows/release.yml` define automated validation and release policy.
5. `.github/CONTRIBUTING.md` defines the contributor and pull request workflow.

When code, tests, documentation, and the specification disagree, do not silently choose one. Identify the conflict and make the smallest coordinated correction.

## Repository map

- `header.go`, `frame.go`: fixed-header and complete-frame encoding, parsing, validation, and size limits.
- `tlv.go`, `messages.go`: canonical TLV and typed message codecs.
- `crypto.go`, `handshake.go`: AEAD framing and the three-flight Noise XX handshake.
- `session.go`, `replay.go`, `rekey.go`: authenticated session state, replay protection, epochs, and rekey transitions.
- `transport.go`, `tcp.go`: framed transport contracts and TCP implementation.
- `connection.go`, `server.go`: concurrent connection runtime, dispatch, admission, shutdown, and server limits.
- `keylog.go`: opt-in local traffic-key logging controlled by `DGPROTO_KEYLOG`; never enable it in production or expose its output.
- `conformance_test.go`, `mvp_constraints_test.go`: normative protocol and MVP guardrails.
- `testdata/vectors/`, `wire_vectors_test.go`: deterministic cross-implementation fixtures and round-trip checks.
- `parser_fuzz_test.go`: fuzz targets for headers, frames, TLVs, messages, and TCP framing.
- `benchmark_test.go`: wire-size and codec performance benchmarks.
- `docs/`: architecture, usage guidance, and the normative protocol specification.
- `.github/workflows/`: CI and maintainer-only release automation.

## Non-negotiable protocol and security invariants

Treat framing, parsing, authentication, replay, rekey, and lifecycle changes as security-sensitive.

- The strict MVP is TCP + three-flight `Noise_XX_25519_ChaChaPoly_SHA256` + ChaCha20-Poly1305. Do not introduce negotiation, alternative suites, resumption, 0-RTT, or obfuscation through the MVP API.
- Preserve exact protocol bytes: `DGP1`, version `1`, 40-byte little-endian header, no outer length prefix, 65535-byte maximum frame, and a 16-byte tag on encrypted frames.
- Senders zero reserved flags/bytes; only `FlagPadding` is sendable. Receivers preserve unknown flags and reserved bytes, and authenticate the exact received header.
- Cleartext random padding is 0–255 bytes and is included with the header as AEAD AAD. Do not weaken its authentication or describe it as transport obfuscation.
- Handshake frames use zero session ID/sequence and no outer AEAD tag. Preserve prologue `DGPv1`, Noise flight ordering, directional Split keys, and session ID derivation from `SHA-256("DGPv1 SessionID" || channel binding)[:16]`.
- Sequences start at 1. The nonce is four zero bytes followed by the little-endian sequence. Never permit key/nonce reuse.
- Replay checks precede authentication; replay state commits only after successful authentication. Keep receive rekey state changes atomic.
- Rekey epochs start at 1 and advance by exactly one. Preserve the `1<<32` frame/10-minute trigger, send-before-commit `RekeyInit` ordering, labels, constant-time confirmation, and 2048-frame/30-second previous-key grace bounds.
- Message type `0x07` and post-MVP flags remain reserved and are rejected by `Session`.
- Do not log, commit, or fixture real static private keys, credentials, user data, or production captures. Applications must enforce peer admission; Noise authentication alone is not authorization.
- Parsers must reject hostile/truncated input without panic, enforce limits, preserve promised ownership, and retain deterministic canonical encoding.
- Preserve documented concurrency and shutdown guarantees. `Session` exported methods are concurrent-safe; do not assume `Handshake` or `ReplayWindow` independently are.
- `SendAndWait` confirms a local transport write, not peer receipt or processing.

Report vulnerabilities privately as described in `SECURITY.md`; never disclose suspected vulnerabilities in public issues or pull requests.

## Go and API conventions

- Target the Go version declared in `go.mod`; keep code `gofmt`-clean and idiomatic.
- Preserve zero-value and default behavior documented by exported types. Treat changes to exported names, interfaces, errors, defaults, ownership, blocking, and cancellation as API changes.
- Wrap errors only when callers can still use `errors.Is` or `errors.As`; keep existing `dgproto:` wording and sentinel semantics consistent.
- Do not panic on peer-controlled input. Return precise errors and preserve parser bounds and ownership guarantees.
- Keep exported methods concurrency-safe where documented. Do not add unbounded goroutines, queues, retries, or shutdown waits.
- Do not add dependencies, algorithms, protocol negotiation, generated code, `unsafe`, or pooling without a demonstrated requirement and explicit review.
- Keep comments focused on contracts, invariants, and non-obvious decisions rather than restating the code.

## Change workflow

1. Read the relevant specification section, implementation, callers, tests, vectors, API documentation, and CI before editing.
2. Classify impact on wire bytes, cryptographic derivations, state transitions, concurrency, public API, resource bounds, and interoperability.
3. Add a focused failing regression test before the fix when correcting a defect.
4. Make the smallest coherent change; do not alter constants, labels, layouts, limits, defaults, or error semantics speculatively.
5. Cover success, malformed input, boundaries, and invalid state order. For lifecycle code, cover cancellation, queue bounds, shutdown, and the first terminal error.
6. For parser or message changes, update applicable fuzz seeds and vectors. New or revised wire behavior requires coordinated valid and invalid vectors; valid vectors must decode and re-encode byte-for-byte.
7. Update specification, package documentation, guides, and examples in the same change whenever their contracts are affected.
8. Review the final diff for secrets, key logs, generated artifacts, module drift, unrelated edits, and accidental protocol changes.

## Validation commands

Run commands from the repository root. During development, format changed Go files and run the narrowest relevant test first. Before submitting any code change, run the baseline:

```sh
gofmt -l .
go test -count=1 ./...
go vet ./...
```

`gofmt -l .` must produce no output. Match CI more closely with:

```sh
go mod tidy
git diff --exit-code -- go.mod go.sum
go test -race -count=1 ./...
govulncheck ./...
```

The race detector requires `CGO_ENABLED=1` and a supported C toolchain. If a required tool or platform is unavailable, state exactly what was not run; never claim a check passed when it was skipped. CI runs ordinary tests on Linux, Windows, and macOS, plus Linux race and vulnerability jobs.

## Testing guidance

- Use same-package tests (`package dgproto`) when coverage requires internal state; otherwise test observable behavior through exported APIs.
- Run `go test -run TestJSONWireVectors -count=1` for vector changes and preserve byte-for-byte decode/re-encode checks.
- For parser or framing changes, run the applicable target in `parser_fuzz_test.go`, for example `go test -run=^$ -fuzz=FuzzFrameUnmarshalBinary -fuzztime=60s`. Keep useful minimized findings as Go fuzz corpus entries under `testdata/fuzz/` when appropriate.
- For session, replay, rekey, connection, server, or transport changes, test concurrency, cancellation, queue saturation, partial I/O, and shutdown, then run the race suite when supported.
- For performance-sensitive changes, run `go test -bench=. -benchmem -benchtime=5s` and add or update a representative benchmark when behavior warrants it.
- Tests must be deterministic and bounded. Avoid sleeps when synchronization can observe the intended state.
- Never weaken authentication, replay checks, limits, constant-time comparisons, race coverage, or vectors merely to make a test pass.

## Documentation, compatibility, and releases

- Any change to wire bytes, message types, state transitions, limits, or cryptographic derivations must update `docs/protocol/dgproto-v1.md` in the same change and receive compatibility and security review.
- The specification is still a draft, but incompatibility must remain explicit. A backward-incompatible protocol change requires a deliberate versioning decision and coordinated specification, vectors, implementation, and documentation updates.
- Update README, architecture docs, guides, examples, and package documentation when public API, integration steps, defaults, limits, ownership, or user-visible behavior changes. Do not document unsupported features.
- Preserve public behavior unless an intentional API or specification change is part of the task. Add migration guidance for breaking Go API changes.
- Do not edit release tags, publish modules, create releases, or modify release credentials as part of ordinary development. `.github/workflows/release.yml` is maintainer-owned release automation.

## Security and repository hygiene

- Never commit credentials, private keys, traffic keys, key-log output, production captures, user data, or private vulnerability details.
- `DGPROTO_KEYLOG` is for explicit local debugging only. Keep it unset in tests, CI, examples, and production.
- Report suspected vulnerabilities only through the private process in `SECURITY.md`; do not expose exploit details in issues, pull requests, commits, or fixtures.
- Do not commit local plans, scratch files, editor output, binaries, coverage files, fuzz executables, or generated transcripts.
- Do not overwrite or discard unrelated working-tree changes. Stage only files that belong to the requested change.

## Contribution quality

- Keep changes focused and use clear, conventional commit messages.
- In the pull request, describe motivation, behavioral impact, validation, compatibility, and security implications where relevant.
- Keep public documentation factual, portable, and user-focused. Do not include machine-specific paths or tool-session details.
- Follow `.github/CONTRIBUTING.md`; do not bypass required checks. Commit or publish only when the task explicitly requests it and the contributor has the necessary access.

## Definition of done

- Implementation, tests and vectors, normative specification, exported documentation, and user-facing documentation agree.
- The smallest relevant regression test exists and the baseline passes; applicable race, fuzz, benchmark, platform, and vulnerability checks pass or omissions are reported precisely.
- `go mod tidy` produces no module diff, `gofmt -l .` produces no output, and `git diff --check` passes.
- The final diff contains no secrets, key material, artifacts, unrelated edits, accidental public API changes, or unexplained wire changes.
