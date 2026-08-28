# Contributing to DGProto for Go

DGProto is a security-sensitive protocol library, not an application server.
This guide covers the Go module `github.com/datagram-messenger/dgproto-go`.
The [DGProto v1 specification](docs/protocol/dgproto-v1.md) is normative for
wire-visible behavior.

## Prerequisites and setup

Use Go 1.25 or later, as required by `go.mod` and CI.

```sh
git clone https://github.com/datagram-messenger/dgproto-go.git
cd dgproto-go
go mod download
```

Run commands from the repository root. Race-detector runs additionally require
`CGO_ENABLED=1` and a supported C toolchain.

## Repository map

The module contains one package, `dgproto`:

- `header.go`, `frame.go`, and `tlv.go`: wire framing and TLV encoding;
- `messages.go`: protocol message encoding and validation;
- `crypto.go`, `handshake.go`, `session.go`, `replay.go`, and `rekey.go`:
  cryptography and secure session state;
- `transport.go`, `tcp.go`, `connection.go`, and `server.go`: TCP transport and
  concurrent connection lifecycle;
- `docs/protocol/dgproto-v1.md`: normative wire specification;
- `docs/architecture/overview.md` and `docs/guides/getting-started.md`: design
  and integration documentation;
- `testdata/vectors/` and `wire_vectors_test.go`: interoperability vectors;
- `parser_fuzz_test.go`: parser and transport fuzz targets;
- `.github/workflows/ci.yml` and `.github/workflows/release.yml`: authoritative
  automated checks.

## Change workflow

1. Read the relevant specification, implementation, callers, tests, vectors,
   and CI before changing behavior.
2. Identify effects on wire bytes, limits, cryptographic derivations, state
   transitions, concurrency, exported API, and interoperability.
3. Make the smallest coherent change. Add focused tests for successful use,
   malformed input, boundaries, and invalid state ordering as applicable.
4. Update the normative specification and public documentation when their
   documented behavior changes.
5. Run the baseline and applicable checks below. Inspect the final diff for
   module drift, secrets, artifacts, and unrelated edits.

## Coding, API, and compatibility guidelines

- Format Go code with `gofmt`; follow Go naming and godoc conventions. Package
  documentation is in `doc.go`.
- Preserve ownership, deterministic encoding, limits, and typed errors. Follow
  existing `dgproto:` wrapping and sentinel conventions where callers need
  `errors.Is` or `errors.As`.
- Parsers must reject hostile, malformed, oversized, and truncated input without
  panicking.
- Treat exported API changes as compatibility changes. The module is pre-v1,
  but intentional breaking changes still require tests, documentation, and
  maintainer review; do not make them incidentally.
- Preserve documented concurrency and shutdown contracts. `Session` exported
  methods are concurrent-safe; do not infer the same guarantee for `Handshake`
  or `ReplayWindow`. `SendAndWait` confirms a local transport write, not peer
  receipt or processing.
- Do not introduce unsupported negotiation, cipher suites, resumption, 0-RTT,
  or obfuscation into the strict MVP API.

## Protocol and cryptography safety

Framing, parsing, authentication, replay, rekey, and lifecycle changes require
security and compatibility review. In particular:

- preserve the fixed TCP and `Noise_XX_25519_ChaChaPoly_SHA256` profile;
- preserve exact header bytes, little-endian encoding, protocol limits,
  authenticated padding, nonce construction, sequence and replay rules, and
  rekey ordering;
- authenticate exact received wire data, including reserved header fields;
- use constant-time comparison for secret-dependent confirmation values;
- never log or commit real private keys, credentials, user data, or production
  captures; peer authorization remains the application's responsibility.

Any change to wire bytes, message types, limits, or cryptographic derivations
must update [`docs/protocol/dgproto-v1.md`](docs/protocol/dgproto-v1.md) in the
same contribution. The draft specification is versioned independently from the
Go module, so incompatible changes must be explicit rather than silently
altering existing normative behavior.

## Testing

### Required baseline

```sh
gofmt -l .
go test -count=1 ./...
go vet ./...
go mod tidy
git diff --exit-code -- go.mod go.sum
```

`gofmt -l .` must produce no output, and tidying must not change `go.mod` or
`go.sum`. CI also runs ordinary tests on Windows.

### Change-specific and full checks

```sh
# Handshake behavior
go test -count=1 -run '^TestHandshake' ./...

# MVP profile and wire-vector corpus
go test -count=1 -run '^(TestP0MVP|TestJSONWireVectors)' ./...

# Concurrency, transport, session, rekey, connection, or server changes
CGO_ENABLED=1 go test -race -count=1 ./...

# Performance-sensitive changes
go test -bench=. -benchmem -benchtime=5s ./...
```

Run race tests for applicable changes when the platform supports them. CI runs
`govulncheck ./...`; run it locally only when `govulncheck` is already installed.

For parser or wire changes, run an applicable target from
`parser_fuzz_test.go`. Existing targets cover headers, frames, TLVs, messages,
and TCP frame reads. For example:

```sh
go test -run=^$ -fuzz=FuzzFrameUnmarshalBinary -fuzztime=60s .
```

Retain a fuzz failure as a regression test or corpus input only when it is
minimal, reproducible, non-sensitive, and relevant to the parser contract.

## Wire vectors, documentation, and benchmarks

The JSON corpus in `testdata/vectors/` follows
[`testdata/vectors/README.md`](testdata/vectors/README.md). Vector names are
unique, `wire_hex` is lowercase canonical hex, valid vectors decode and
re-encode byte-for-byte, and invalid vectors fail with their named sentinel.
Parser fixes and new message encodings should include applicable valid and
invalid vectors.

Update godoc and the relevant file under `docs/` when exported API, integration,
limits, or user-visible behavior changes. Do not document unsupported features.
For performance-sensitive work, use `benchmark_test.go` and include
representative before/after results in the PR.

## Pull requests

Keep commits and the PR focused. Explain what changed, why, compatibility and
security impact, and the checks run. Link a relevant issue when one exists.

Before requesting review, confirm that:

- [ ] implementation, tests/vectors, normative specification, and public docs
      agree;
- [ ] baseline and applicable race, fuzz, and benchmark checks passed, or an
      environment limitation is recorded;
- [ ] exported API and wire compatibility effects are explicit;
- [ ] `go mod tidy` produces no module-file diff;
- [ ] the diff contains no secrets, generated artifacts, or unrelated changes.

## Reporting security issues

Do not report suspected vulnerabilities in a public issue, discussion, or pull
request. Follow the private disclosure process in [`SECURITY.md`](SECURITY.md).
Do not include real private keys, credentials, or user data in a report.
