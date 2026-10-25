# Incremental package layout

This is the **current** reorganization checkpoint, not a release checklist or
compatibility certification. The [runtime overview](overview.md),
[repository guide](../../AGENTS.md), [contribution policy](../../.github/CONTRIBUTING.md)
and [protocol specification](../protocol/dgproto-v1.md) describe current contracts.
The [archived chronology](package-layout-history.md) preserves earlier observations
and proposals; its inventories, "current" headings and next-step recommendations
are historical, not instructions to implement them.

## Current checkpoint: optional traffic-secret sink

`internal/keylog/` now owns the complete optional process-wide sink: environment
lookup, open-once append file, initialization and leaf write lock, and exact
record formatting. Both implementation and colocated sink tests require
`dgproto_keylog`; default builds still compile the unchanged root no-ops without
importing this leaf. Root tagged functions only forward primitive arrays/scalars.
Session initial-key and committed-rekey call sites, state and locks are unchanged.

This is an independent I/O/debugging responsibility, not a crypto/session split
or another generic utility package. Existing leaves do not own files or traffic
logging. All five leaves are stdlib-only and never import root; four participate
in default builds. No public type, sentinel, dependency or protocol algorithm moves.

Colocated serial tests cover literal synthetic records, append and one-time path
selection, absent/failed-open no-retry, and 32 concurrent initial-key pairs plus
epoch writes. Root rekey integration retains its private Session helpers, using
an isolated subprocess rather than exposing production reset/close hooks; it also
rejects logging a pending epoch before commit. The default no-write regression
remains root. Sink test cleanup closes files before removing temporary directories
on Windows; production still keeps the file open for its process lifetime.

## Previous checkpoint: public consumer contract suite

Local commit `598ca84c2b27d317fc6b778a5d5daec07c3f8dcf` moved the complete
external facade/codec consumer suite to `integrationtests/api/`: **six unchanged
files, 894 lines, 20 top-level tests and two benchmarks**, including their complete
helper closure. Package declarations remain `dgproto_test`; imports still use the
public module. No production source was changed. The 251-entry global inventory
of tests, benchmarks, fuzz targets and godoc examples was preserved.

This creates one independently selectable consumer-contract layer alongside the
existing public TCP probes, without scattering mutable-sentinel tests or exporting
test seams. [Integration test ownership](../../integrationtests/README.md) records
selectors, helper ownership and non-parallel sentinel requirements.

| Responsibility | Current owner and checks |
| --- | --- |
| Public concrete types, validation, errors and owned buffers | Root `dgproto`; Header, TLV, replay and typed-message contracts in `integrationtests/api/` |
| Primitive header/control byte layouts | Stdlib-only `internal/wire/`, with colocated algorithm/golden/precondition tests |
| Bounded TLV arithmetic and byte codec | Stdlib-only `internal/tlv/`, with colocated hostile-input/alignment tests |
| Replay validation/commit/bitmap algorithm | Stdlib-only `internal/replay/`; public storage and Session locking/transactions remain root |
| Accept retry classification | Stdlib-only `internal/acceptpolicy/` |
| Optional process-wide traffic-secret sink | Tagged stdlib-only `internal/keylog/`; root owns session commit decisions and default no-ops |
| Handshake, Session, Connection and Server state/lifecycle | Root responsibility-oriented files; white-box tests remain root |
| Public-only framing rejection/EOF/cancellation | `integrationtests/tcp_test.go` |
| Godoc, vectors, conformance, fuzz and keylog build variants | Existing root examples/tests and unchanged `testdata/` |

The four default leaves and optional tagged keylog leaf remain stdlib-only and acyclic. The mature public root
package is intentional: concrete types are not hidden behind aliases, runtime
ownership is not divided into microscopic packages, and no new interfaces were
introduced merely to force a move.

## Evidence-based boundaries

The import/helper audit rejected moving network-looking root tests:

- `client_integration_test.go` uses injected `dialContext`, private started/key
  storage and shared root helpers.
- `tcp_concurrent_echo_test.go` uses private abort for bounded runtime cleanup.
- `tcp_test.go` supplies `testTransportFrame` to root hardening/session tests.
- `send_api_test.go` exercises private queue/completion state despite its name.

Those tests stay root; no helper duplication or weakened assertions. Keep
`client_example_test.go` root for godoc. Future moves must transfer a genuine
independent helper closure, not merely rename files or split them by line count.

Root StaticKey, Header, Message/TLV/ReplayWindow concrete identity, method sets,
current/nil sentinels, validation priority, allocations/copies and ownership are
preserved. Session authentication/replay/rekey atomicity and Connection/Server
lifecycle/callbacks are untouched. The ErrorMessage specification says
`code:u16 little-endian || context:utf8`, while the existing implementation uses an
optional aligned type-1 text TLV. That discrepancy remains unresolved; existing
byte tests are not a decision to certify the implementation as normative.

## Observed validation and remaining gates

Go 1.26.5 windows/amd64, CGO_ENABLED=1; DGPROTO_KEYLOG unset. After the suite move:

```sh
go test -count=1 ./integrationtests/...
go test -run 'TestJSONWireVectors|TestWIRE|TestHS|TestSESS|TestP0' -count=1 ./...
go test -count=1 ./...
go vet ./...
go test -tags dgproto_keylog -count=1 ./...
go vet -tags dgproto_keylog ./...
go test -race -count=1 ./...
gofmt -l .
git diff --check
```

All passed; formatting/diff output was empty. Full race covered all seven
packages with process-local `C:/msys64/mingw64/bin` PATH and GCC 16.2.0 (MSYS2
Rev4). Git blob equality established all six moves as byte-identical. Plain root
`go test` no longer selects these contracts; use `./integrationtests/...` or `./...`.

The subsequent current/history documentation separation does not change Go code
or runnable examples. Validate links/anchors, fences and staged diffs for that
prose-only step; no additional full code-test rerun is required.

Minimum-line Go 1.25.x, Linux/macOS execution, fresh fuzz campaigns, sustained
benchmarks, vulnerability scans and independent interoperability/deployment
validation remain separate gates, not inferred from these local results. There
are no dependency/fixture changes, release artifacts, push or tags in this batch.
