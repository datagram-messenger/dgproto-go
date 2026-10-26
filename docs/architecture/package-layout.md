# Incremental package layout

This is the **current** reorganization checkpoint, not a release checklist or
compatibility certification. The [runtime overview](overview.md),
[repository guide](../../AGENTS.md), [contribution policy](../../.github/CONTRIBUTING.md)
and [protocol specification](../protocol/dgproto-v1.md) describe current contracts.
The [archived chronology](package-layout-history.md) preserves earlier observations
and proposals; its inventories, "current" headings and next-step recommendations
are historical, not instructions to implement them.

## Current checkpoint: completed responsibility layout

The current layout is complete for this refactor: root `dgproto` imports
`internal/wire`, `internal/tlv`, `internal/replay` and `internal/acceptpolicy`
in default builds, and also `internal/keylog` with `dgproto_keylog`. Each leaf
imports only stdlib, never root. Root retains public concrete identity, current
mutable Magic/error variables, validation/copies and atomic session/runtime state.
`client_config.go`, `server_config.go`, `static_key.go`, the message variants and Session/Connection
file splits organize responsibilities within root; they are not new packages.

Stop cosmetic extraction here. Another package needs an independent cohesive
owner, a complete helper/validation closure and meaningful tests, with evidence
that the boundary preserves contracts and justifies adaptation/performance cost.
File length or package count alone is not a reason to split TCP lanes, handshake,
Session transactions or Connection/Server lifecycle.

### Optional traffic-secret sink

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

## Current ownership

The independently selectable public consumer-contract layer in
`integrationtests/api/` uses `package dgproto_test`. Complete Header validation
and wire-layout tests and TLV public limit/ownership/reference-encoder suites,
including both TLV benchmarks and their local helpers, now live there too.
The top-level `integrationtests/` layer owns public TCP and established-runtime
send/I/O/admission contracts in `package integrationtests`. Avoid stale fixed
file counts; source inventories are not executed-test counts. Mutable-sentinel tests stay serial; no test seams
are exported. [Integration test ownership](../../integrationtests/README.md) records
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
| Public TCP framing/stream/cancellation | `integrationtests/tcp_test.go`, `integrationtests/tcp_stream_test.go`, `integrationtests/tcp_cancellation_test.go`; shared public helpers in `integrationtests/tcp_helpers_test.go` |
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
- `tcp_test_helpers_test.go` supplies `testTransportFrame` and gated-I/O fixtures
  to root white-box consumers; root `tcp_test.go` keeps nil Connection misuse.
  Public stream/cancellation closures are in `integrationtests/`, not these helpers.
- `send_api_test.go` exercises private queue/completion state despite its name.

Those tests stay root; no helper duplication or weakened assertions. Keep
`client_example_test.go` root for godoc. Future moves must transfer a genuine
independent helper closure, not merely rename files or split them by line count.

Root StaticKey, Header, Message/TLV/ReplayWindow concrete identity, method sets,
current/nil sentinels, validation priority, allocations/copies and ownership are
preserved. Session authentication/replay/rekey atomicity and Connection/Server
lifecycle/callbacks remain root-owned. ErrorMessage in `messages_error.go` now
implements normative `code:u16 LE || context:raw UTF-8`, not TLV;
`integrationtests/api/messages_error_api_test.go` owns its public contracts.
The historical discrepancy was resolved by `5d4d34c`. Nonempty Error is
wire-incompatible with the old TLV codec, without negotiation or fallback;
SessionClose remains TLV-backed. See the [migration guide](../guides/error-message-migration.md)
for paired rollout/rollback and Rust's preserved public u8 restriction.
This is an existing compatibility change, not fresh tests or interoperability proof.

## Historical validation and remaining gates

At the public consumer-suite relocation checkpoint
`598ca84c2b27d317fc6b778a5d5daec07c3f8dcf`, six unchanged files, 894 lines,
20 top-level tests and two benchmarks moved with their helper closure;
the 251-entry test/benchmark/fuzz/example inventory was preserved then.
These are historical counts, not the current inventory above.
Go 1.26.5 windows/amd64, CGO_ENABLED=1; DGPROTO_KEYLOG unset. After that move:

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

At that historical checkpoint all passed; formatting/diff output was empty.
Full race covered all seven packages with process-local `C:/msys64/mingw64/bin` PATH and GCC 16.2.0 (MSYS2
Rev4). Git blob equality established all six moves as byte-identical. Plain root
`go test` no longer selects these contracts; use `./integrationtests/...` or `./...`.

## Fresh completion-batch validation

The client configuration closure and four complete public primitive test/benchmark
closures were implemented and reviewed against `65daebc`, not just documented.
PowerShell source comparison confirms the normalization function and all moved
helper/test/benchmark bodies are identical; only package/imports change for tests.
No parser/leaf algorithm or concurrency code changes, so no fuzz campaign was
required for this batch (full tests execute seeds, not campaigns).

Go 1.26.5 windows/amd64, CGO_ENABLED=1. Focused pre-change Header/TLV/DialContext
tests passed. Post-change focused ClientConfig/DialContext/Header/TLV tests,
full uncached default and `dgproto_keylog` tests, both vet configurations, and
JSON vectors/WIRE/HS/SESS/P0 selectors passed. Full default and tagged race passed
with process-local MinGW PATH and GCC 16.2.0 (MSYS2 Rev4). Initial post-move
compilation found stale `errors`/`time` imports in `client.go`; those imports were
removed, then gates rerun without weakening tests. Formatting and both whitespace
diffs were empty; index remains empty and no Git publication action was taken.

`BenchmarkEncodeTLVsPatch` ran before/after with `-run '^$' -benchmem
-benchtime=5s -count=3`, at root before and `./integrationtests/api` after.
All shapes retain one allocation: Long 65536 B, Typical 192 B, ManyEmpty 16384 B.
Before medians were 20924/124.7/31868 ns; after medians 23182/120.8/32242 ns.
The first after Long sample (35455 ns) overlapped the race gate and is confounded;
these measurements establish unchanged allocation budgets, not an optimization or
statistically established CPU regression. Production TLV code/boundary is unchanged.

Structural stop condition: no currently justified additional production package
split remains. TCP lane/watch closure and Session/Connection/Server transactions
remain whole; client/server settings keep their distinct validation policies.
Root TLV alignment, extended-message dispatch, injected client dialers and private
runtime/crypto fixtures cannot move wholesale to the public layer. Future optional
public suite moves need a fresh complete-helper audit, not individual-test scattering.
Wider production assurance still needs security-patched minimum-Go/other-OS CI,
independent exact-revision interoperability (including Error migration), security
and dependency review, sustained adversarial fuzzing, and measured service budgets
for load/saturation/shutdown/callbacks; directory completion cannot certify these.

Minimum-line Go 1.25.x, Linux/macOS execution, fresh fuzz campaigns, sustained
benchmarks, vulnerability scans and independent interoperability/deployment
validation remain separate gates, not inferred from these local results. There
are no dependency/fixture changes, release artifacts, push or tags in this batch.
