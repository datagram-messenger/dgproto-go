# Contributing

## Scope

Read the repository's [coding agent guide](../AGENTS.md) and the normative [DGProto v1 specification](../docs/protocol/dgproto-v1.md) before making changes. Keep wire changes, implementation, tests, vectors, and documentation synchronized. The canonical repository path is `github.com/datagram-messenger/dgproto-go`.

## Where to add code and tests

Start with the [repository map](../AGENTS.md#repository-map) and [architecture](../docs/architecture/overview.md). Keep public definitions in root `dgproto`: source files organize cohesive responsibilities, not separate Go packages. Handshake wrapper types/codecs belong in `messages_handshake.go`; shared sizes/errors and application/control families remain in `messages.go`. Put focused tests beside the implementation (`*_test.go`); use `package dgproto_test` for consumer-visible identity, interface, error and buffer-ownership contracts, and root white-box tests for atomic state/locking invariants. Existing vectors and conformance tests remain shared integration coverage.

The public facade owns its concrete types and error sentinels, not aliases to internal runtime state. The dependency direction is root → `internal/acceptpolicy` → standard library; internal leaves must not import root. Add an internal package only for a cohesive independent responsibility with explicit ownership and meaningful tests, not to imitate an application/server directory tree. Session replay/rekey transactions and connection/server lifecycle remain root-owned. Preserve imports, method sets, concrete type identity, `errors.Is`/`errors.As`, copy/alias behavior and keylog build variants. A source move is not production certification.

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
