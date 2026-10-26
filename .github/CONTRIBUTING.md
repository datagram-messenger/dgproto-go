# Contributing

## Scope

Read the repository's [coding agent guide](../AGENTS.md) and the normative [DGProto v1 specification](../docs/protocol/dgproto-v1.md) before making changes. Keep wire changes, implementation, tests, vectors, and documentation synchronized. The canonical repository path is `github.com/datagram-messenger/dgproto-go`.

## Where to add code and tests

Start with the [repository map](../AGENTS.md#repository-map) and [architecture](../docs/architecture/overview.md), treating inventories as descriptions, not fixed package or filename policies. Preserve the existing public API, wire behavior, error chains, buffer ownership and atomic session/runtime transactions. Change boundaries only for concrete needs, not package counts or file length; avoid recurrent cosmetic moves and test-only production seams.

Put private-state tests beside their implementation. Use external-package tests for public contracts, either at root or in the selectable `integrationtests/` suites. Keep godoc examples discoverable and shared fixtures with their users; do not duplicate helpers or weaken useful assertions to make a move. Mutable-sentinel tests must remain serial. `go test -count=1 ./...` covers all packages; plain root `go test` does not cover the integration subpackages.

Default builds must exclude the traffic-secret sink and never write keylogs. The build check is in `keylog_build_test.go`, the no-write regression in `integrationtests/api/keylog_disabled_test.go`, and tagged sink/Session tests verify enabled behavior with synthetic data and subprocess isolation. New internal packages or shared dependencies need explicit ownership and relevant tests, not an allowlist update.

ErrorMessage currently uses `code:u16 LE || context:raw UTF-8`, not the historical TLV codec. Nonempty Error is wire-incompatible without negotiation or fallback; follow the [paired migration/rollback guide](../docs/guides/error-message-migration.md), including Rust's public u8 restriction. Test placement does not establish interoperability or production readiness.

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

Current [CI](workflows/ci.yml) runs default tests on Linux/Windows/macOS, Linux default/tagged race, tagged tests/vet, formatting, tidy and vulnerability checks. The [release workflow](workflows/release.yml) validates default/tagged tests, race and vet on Linux plus an external import smoke test. Neither workflow runs fuzz campaigns, benchmarks, independent interoperability or signing. Run risk-relevant local checks and report missing platform/security evidence; publication remains an explicitly authorized maintainer task.

## Pull requests

Describe motivation, behavioral impact, validation, protocol compatibility, and follow-up work. Keep changes focused and identify normative specification changes explicitly. Report vulnerabilities through the [security policy](../SECURITY.md), not a public issue.
