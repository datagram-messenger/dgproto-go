# Repository structure plan

## Current state

The repository is a single-package Go library with production code and focused tests at the module root. Normative protocol documentation is isolated under `docs/protocol/`, explanatory documentation is grouped under `docs/architecture/` and `docs/guides/`, interoperability fixtures live under `testdata/vectors/`, and CI and release automation live under `.github/workflows/`.

This layout already supports package discoverability, Go tooling, external-package consumption, protocol review, fuzzing, race testing, module hygiene, vulnerability scanning, and tagged releases. No evidence supports splitting the package or relocating source, tests, module files, license, security policy, or normative specification.

The contribution guide was at the repository root. GitHub recognizes community-health files in `.github/`, `docs/`, or the root, so `.github/` can hold contribution guidance without affecting Go tooling while keeping the root focused on module entry points.

## Target structure

```text
.github/
  CONTRIBUTING.md
  workflows/
docs/
  architecture/
    overview.md
    repository-structure-plan.md
  guides/
  protocol/
testdata/
  vectors/
*.go
*_test.go
go.mod
README.md
SECURITY.md
LICENSE
```

## Rationale

- Keep the implementation as one root package because the public API and internal protocol/runtime components are tightly integrated; speculative subpackages would add compatibility and navigation costs.
- Keep tests beside the package they exercise, following Go conventions. Retain reusable wire fixtures under `testdata/`, which Go tooling excludes from packages while tests can access it.
- Keep the normative specification in `docs/protocol/` and clearly distinguish it from non-normative architecture and usage documentation.
- Place contribution guidance in `.github/` as a supported community-health location and update links relative to its new location.
- Keep `README.md`, `SECURITY.md`, `LICENSE`, and `go.mod` at the root for immediate discovery by users and tooling.

## Completed changes

- Moved `CONTRIBUTING.md` to `.github/CONTRIBUTING.md` with Git history preserved.
- Updated contribution-guide references to the repository-local `AGENTS.md`, `SECURITY.md`, and the normative specification for the new location.
- Updated the root README contribution link.
- Added this tracked audit and plan; no production code, package layout, test fixtures, workflows, or normative protocol text changed.

## Deferred proposals

- Issue and pull-request templates are deferred until the project defines the questions, labels, and triage process they must enforce; generic templates would invent process and add maintenance burden.
- `CODEOWNERS` is deferred until maintainers explicitly provide ownership boundaries and valid GitHub users or teams.
- A separate release or maintenance policy is deferred because the existing release workflow and security policy describe current behavior, and no additional supported-version or release cadence policy is established.
- Package splitting and creation of `internal/` or `cmd/` trees are deferred until concrete reusable boundaries or binaries exist.
- CI changes are deferred because current workflows already cover tests, vet, formatting, module tidiness, race detection, Windows, vulnerability scanning, external consumption, and release validation.

## Risks

- Links can become stale after moving a document. Validate every relative Markdown target and same-page anchor.
- GitHub discovers `.github/CONTRIBUTING.md`, but repository-local references must use the explicit path.
- Structural churn can obscure protocol changes. Keep this work documentation-only and verify that the normative specification and Go sources are untouched.
- Local ignored planning files can be accidentally staged. Confirm `V1_IMPLEMENTATION_PLAN.md` remains ignored and absent from the tracked diff.

## Verifiable criteria

- The active local branch is `docs/repository-structure`, based on the prior `docs/organize-protocol` commit so unique local history is retained.
- The local `docs/organize-protocol` branch no longer exists; no remote refs are modified.
- All work remains uncommitted in the working tree.
- `git diff --check`, Markdown link/anchor and fence validation, `gofmt -l .`, `go test -count=1 ./...`, and `go vet ./...` pass.
- `V1_IMPLEMENTATION_PLAN.md` remains ignored, present locally, untracked, and absent from the diff.
- The tracked diff contains only the contribution-guide move, corrected links, and this plan; no normative specification or Go implementation changes are present.
