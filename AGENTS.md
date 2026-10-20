# Repository Guide for Coding Agents

Welcome to `dgproto-go`. This document gives coding agents and contributors the project-specific context needed to make safe, reviewable changes. It applies to the entire repository unless a more specific `AGENTS.md` exists in a subdirectory.

## Project overview

`dgproto-go` is the Go 1.25+ implementation of the strict DGProto v1 MVP wire protocol and secure session runtime. It is a protocol library, not the application server.

Start with [`README.md`](README.md) for capabilities and usage. For wire-visible behavior, [`docs/protocol/dgproto-v1.md`](docs/protocol/dgproto-v1.md) is normative. Read [`SECURITY.md`](SECURITY.md) before reporting or handling a suspected vulnerability.

## Repository map

- `header.go`, `frame.go`, `tlv.go`, `messages.go`: wire encoding and parsing.
- `crypto.go`, `handshake.go`, `session.go`, `replay.go`, `rekey.go`: cryptographic and session state.
- `tcp.go`, `connection.go`, `server.go`: transport and concurrent lifecycle.
- `testdata/vectors/`, `wire_vectors_test.go`: strict interoperability vectors.
- `parser_fuzz_test.go`: parser and transport fuzz targets.
- `.github/workflows/ci.yml`: CI matrix and exact baseline checks.

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

## Change workflow

1. Read the relevant specification section, implementation, callers, tests, vectors, and CI before editing.
2. Classify impact on wire bytes, derivations, state transitions, concurrency, public API, and interoperability.
3. Make the smallest coherent change; do not change constants, labels, layouts, limits, or error semantics speculatively.
4. Add focused regression coverage for success, malformed input, boundaries, and invalid state order. For lifecycle code, cover cancellation, queue bounds, shutdown, and terminal errors.
5. For parser/message changes, update applicable fuzz seeds and vectors. Parser fixes and new message types require valid and invalid vectors; valid vectors must decode and re-encode byte-for-byte.
6. Review the final diff for secrets, generated artifacts, module drift, unrelated edits, and accidental protocol changes.

## Validation commands

Run from the repository root. Fast loop:

```sh
gofmt -l .
go test -count=1 ./...
go vet ./...
```

`gofmt -l .` must print nothing. Full CI-equivalent validation:

```sh
go test -count=1 ./...
go vet ./...
gofmt -l .
go mod tidy
git diff --exit-code -- go.mod go.sum
go test -race -count=1 ./...
```

The race detector requires `CGO_ENABLED=1` and a working supported C toolchain. If unavailable, record the reason instead of claiming the race suite passed. CI also runs ordinary tests on Windows and `govulncheck ./...`.

## Testing guidance

- Run focused tests while developing; use `go test -run TestJSONWireVectors ./...` for vector changes.
- For parser/wire changes, run applicable targets from `parser_fuzz_test.go`, for example `go test -fuzz=FuzzFrameUnmarshalBinary -fuzztime=60s`. Preserve useful findings under `testdata/fuzz/`.
- For concurrency, session, rekey, connection, server, or transport changes, run the race suite when supported.
- For performance-sensitive changes, run `go test -bench=. -benchmem -benchtime=5s ./...` and add a representative benchmark when needed.
- Never weaken authentication, replay, limits, constant-time checks, race coverage, or vectors merely to make tests pass.

## Documentation and compatibility

- Any change to wire bytes, message types, or cryptographic derivations must update `docs/protocol/dgproto-v1.md` in the same change and receive compatibility/security review. Backward-incompatible draft changes require a new version section or specification document.
- Update README, architecture docs, and godoc when public API, integration, limits, or user-visible behavior changes. Do not document unsupported features.
- Preserve public behavior unless an intentional API/specification change is requested. Use the existing `dgproto:` error and sentinel conventions when callers need `errors.Is`/`errors.As`.

## Contribution quality

- Keep changes focused and use clear, conventional commit messages.
- Explain compatibility or security implications in the pull request when a change affects protocol behavior, cryptography, exported APIs, or concurrency guarantees.
- Keep public documentation factual and user-focused. Do not include local paths, credentials, private planning notes, generated transcripts, or tool-specific session details.
- Follow the repository's review and contribution process. Do not bypass required checks or publish changes unless the contributor has explicitly requested it and has the necessary access.

## Definition of done

- Implementation, tests or vectors, normative specification, and public documentation agree.
- Focused regression tests cover the change; required baseline and applicable race, fuzz, or benchmark checks pass, or any environment limitation is clearly reported.
- `go mod tidy` produces no module diff, formatting is clean, and the final diff contains no secrets, artifacts, or unrelated changes.
