# Contributing

## Scope

Read the repository's [coding agent guide](../AGENTS.md) and the normative [DGProto v1 specification](../docs/protocol/dgproto-v1.md) before making changes. Keep wire changes, implementation, tests, vectors, and documentation synchronized. The canonical repository path is `github.com/datagram-messenger/dgproto-go`.

## Where to add code and tests

Start with the [repository map](../AGENTS.md#repository-map) and [architecture](../docs/architecture/overview.md). Keep public definitions in root `dgproto`: source files organize cohesive responsibilities, not separate Go packages. Handshake wrapper types/codecs belong in `messages_handshake.go`; shared sizes/errors and primitive control wrappers remain in `messages.go`. TLV-backed EncryptedData/SessionClose and their complete uniqueness/text validation closure belong in `messages_tlv.go`; consumer contracts belong in `integrationtests/api/messages_tlv_api_test.go`. Raw ErrorMessage belongs in `messages_error.go`, with public contracts in `integrationtests/api/messages_error_api_test.go`: `code:u16 LE || context:raw UTF-8`, not TLV. Nonempty Error is wire-incompatible with the historical TLV codec, without negotiation or heuristic fallback; follow the [paired migration/rollback guide](../docs/guides/error-message-migration.md), including Rust's public u8 limit. This describes an existing wire break, not fresh interoperability evidence. Put focused tests beside the implementation (`*_test.go`); use `package dgproto_test` for consumer-visible identity, interface, error and buffer-ownership contracts, and root white-box tests for atomic state/locking invariants. Keep self-contained black-box network scenarios in `integrationtests/`, importing the public library with the explicit `dgproto` alias. `integrationtests/tcp_test.go` covers oversized-header rejection before body reads, EOF/malformed framing, and read-context cancellation using `net.Pipe`. Public stream and lane-cancellation closures live in `integrationtests/tcp_stream_test.go` and `integrationtests/tcp_cancellation_test.go`, with public helpers in `integrationtests/tcp_helpers_test.go`. Root `tcp_test_helpers_test.go` retains `testTransportFrame` and gated-I/O fixtures for white-box consumers; root `tcp_test.go` retains nil Connection misuse. Move complete helper dependency closures only when root white-box tests do not need them; never duplicate helpers, export test seams, or weaken assertions to make a move possible. Public facade regressions live together in `integrationtests/api/` (still `package dgproto_test`, with no private seams); algorithm tests stay beside internal implementations, and white-box runtime/session tests stay root. See [integration test ownership](../integrationtests/README.md). The control and TLV-message tests share `messageSentinel` and must move as one dependency closure; mutable-sentinel tests must stay non-parallel. Keep `client_example_test.go` at root for godoc and the README quick start. Keylog sink tests belong beside tagged `internal/keylog/writer.go`; root Session/rekey integration uses subprocess isolation without exporting reset hooks, and default no-write tests remain root. Deterministic vectors/testdata, conformance and fuzz coverage retain their existing locations. Run `go test -count=1 ./integrationtests/...` for all public integration subareas and `go test -count=1 ./...` for all packages.

The public facade owns its concrete types and error sentinels, not aliases to internal runtime state. Root imports `internal/wire`, `internal/tlv`, `internal/replay` and `internal/acceptpolicy` by default; only `dgproto_keylog` builds also import `internal/keylog`. All leaves import only the standard library, never root. The [current package layout](../docs/architecture/package-layout.md) is complete for this refactor; stop cosmetic extraction unless a cohesive independent owner and complete tested closure justify the boundary. Root file splits do not require new packages. Put bounded TLV wire/arithmetic algorithm tests in `internal/tlv/codec_test.go`; golden/alignment scenarios live there. Public TLV identity, dynamically replaceable sentinel diagnostics, ownership/empty shape and allocation tests belong in `integrationtests/api/tlv_api_test.go`; root limits/encode regressions, benchmarks, vectors and fuzz tests remain public integration coverage. Preserve the primitive validated reader/writer boundary: no intermediate descriptor slice, extra value copy, aliases or callbacks solely to hide root types. Run focused TLV/vector/conformance tests, a bounded-worker TLV fuzz campaign and matched shape benchmarks when changing this boundary. Put replay validation/commit/bitmap changes and focused algorithm tests in `internal/replay/`; retain public identity, field layout, token/error adaptation and consumer regressions in `replay.go`/`integrationtests/api/replay_api_test.go`. The replay leaf operates on caller-owned primitive storage, so value-copy transactions and locking remain in Session. Add an internal package for a cohesive independent responsibility with explicit ownership and meaningful tests, not merely another root file split. Session replay/rekey transactions and connection/server lifecycle remain root-owned. Preserve imports, method sets, concrete type identity, `errors.Is`/`errors.As`, copy/alias behavior and keylog build variants. A source move is not production certification.

## Local baseline

For code changes, run:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
```

Use focused tests while iterating, then run the complete baseline. Do not modify vector data unless the change explicitly requires it.

## Documentation-only validation

- Check every relative link target and same-page anchor.
- Check balanced Markdown fences and suitable language tags.
- Compile Go examples in a temporary module against the current checkout.
- Compare constants, defaults, guarantees, file names, and terminology with source and the normative specification.
- Run `gofmt` when `doc.go` changes.
- Run `git diff --check` and inspect `git status --short` for artifacts or out-of-scope files.

## Conditional CI and release checks

CI or release jobs may additionally run race, fuzz, interoperability, benchmark, platform, vulnerability, signing, or publication checks. Run the relevant subset locally when changing those areas. These conditional checks do not replace the local baseline; credentialed publication remains a maintainer release task.

## Pull requests

Describe motivation, behavioral impact, validation, protocol compatibility, and follow-up work. Keep changes focused and identify normative specification changes explicitly. Report vulnerabilities through the [security policy](../SECURITY.md), not a public issue.
