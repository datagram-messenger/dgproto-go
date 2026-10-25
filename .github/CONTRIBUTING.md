# Contributing

## Scope

Read the repository's [coding agent guide](../AGENTS.md) and the normative [DGProto v1 specification](../docs/protocol/dgproto-v1.md) before making changes. Keep wire changes, implementation, tests, vectors, and documentation synchronized. The canonical repository path is `github.com/datagram-messenger/dgproto-go`.

## Where to add code and tests

Start with the [repository map](../AGENTS.md#repository-map) and [architecture](../docs/architecture/overview.md). Keep public definitions in root `dgproto`: source files organize cohesive responsibilities, not separate Go packages. Handshake wrapper types/codecs belong in `messages_handshake.go`; shared sizes/errors and primitive control wrappers remain in `messages.go`. TLV-backed EncryptedData/SessionClose/ErrorMessage and their complete uniqueness/text validation closure belong in `messages_tlv.go`; consumer contracts belong in `messages_tlv_api_test.go`. Put focused tests beside the implementation (`*_test.go`); use `package dgproto_test` for consumer-visible identity, interface, error and buffer-ownership contracts, and root white-box tests for atomic state/locking invariants. Keep self-contained black-box network scenarios in `integrationtests/`, importing the public library with the explicit `dgproto` alias. The current `integrationtests/tcp_test.go` covers oversized-header rejection before body reads, EOF/malformed framing, and read-context cancellation using `net.Pipe`. Move complete helper dependency closures only when root white-box tests do not need them; never duplicate helpers, export test seams, or weaken assertions to make a move possible. Public facade regressions remain root `dgproto_test`; algorithm tests stay beside internal implementations, and white-box runtime/session tests stay root. Keep `client_example_test.go` at root for godoc and the README quick start. Keylog-tag tests, deterministic vectors/testdata, conformance and fuzz coverage retain their existing locations. Run `go test -count=1 ./integrationtests` for this network layer and `go test -count=1 ./...` for all packages.

The public facade owns its concrete types and error sentinels, not aliases to internal runtime state. Root imports `internal/tlv`, `internal/acceptpolicy` and `internal/replay`; all leaves import only the standard library, never root. Put bounded TLV wire/arithmetic algorithm tests in `internal/tlv/codec_test.go`; golden/alignment scenarios live there. Public TLV identity, dynamically replaceable sentinel diagnostics, ownership/empty shape and allocation tests belong in `tlv_api_test.go`; root limits/encode regressions, benchmarks, vectors and fuzz tests remain public integration coverage. Preserve the primitive validated reader/writer boundary: no intermediate descriptor slice, extra value copy, aliases or callbacks solely to hide root types. Run focused TLV/vector/conformance tests, a bounded-worker TLV fuzz campaign and matched shape benchmarks when changing this boundary. Put replay validation/commit/bitmap changes and focused algorithm tests in `internal/replay/`; retain public identity, field layout, token/error adaptation and consumer regressions in `replay.go`/`replay_api_test.go`. The replay leaf operates on caller-owned primitive storage, so value-copy transactions and locking remain in Session. Add an internal package for a cohesive independent responsibility with explicit ownership and meaningful tests, not merely another root file split. Session replay/rekey transactions and connection/server lifecycle remain root-owned. Preserve imports, method sets, concrete type identity, `errors.Is`/`errors.As`, copy/alias behavior and keylog build variants. A source move is not production certification.

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
