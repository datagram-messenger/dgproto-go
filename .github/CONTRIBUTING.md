# Contributing

## Scope

When present in your checkout, read the repository-local `AGENTS.md` and the normative [DGProto v1 specification](../docs/protocol/dgproto-v1.md). Keep wire changes, implementation, tests, vectors, and documentation synchronized. The canonical repository path is `github.com/datagram-messenger/dgproto-go`.

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
