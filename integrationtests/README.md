# Public integration tests

## Current ownership

`api/` owns public primitive/codec contracts in `package dgproto_test`;
the top-level TCP/runtime layer uses `package integrationtests`, importing the
public library as `dgproto`. Ownership is defined by dependency closures rather
than fixed file counts. This inventory is not a claim of fresh test results.
The [current package layout](../docs/architecture/package-layout.md) records
the completed root/default-leaf graph and optional tagged keylog boundary.

## Historical relocation evidence

At local commit `598ca84c2b27d317fc6b778a5d5daec07c3f8dcf`, the six-file
relocation preserved all 20 tests and two benchmarks byte-for-byte;
Git blob comparison and the 251-entry global test/benchmark/fuzz/example inventory
were unchanged at that checkpoint, not counts of today's suite.
Historical local validation used Go 1.26.5 windows/amd64, CGO_ENABLED=1:
focused integration tests, vector/conformance selectors, full default/tagged tests
and vet, and full race passed on all seven packages. Race used process-local
MinGW PATH and GCC 16.2.0 (MSYS2 Rev4). Formatting/diff checks were empty;
DGPROTO_KEYLOG was unset. These are local results, not minimum-Go/other-OS,
fuzz-campaign, sustained-benchmark, vulnerability or interoperability evidence.
That historical documentation-only synchronization did not rerun tests;
validation for later implementation/test changes is recorded in the layout history.

## Current selectors and helper boundaries

Run `go test -count=1 ./integrationtests/...` from the repository root. Plain
`go test` at root does not run these packages.

- `api/`: consumer contracts for Header, Frame, TLV, replay and typed handshake/control/
  application messages. Raw Error contracts live in `api/messages_error_api_test.go`
  against root `messages_error.go`; `api/messages_tlv_api_test.go` covers
  TLV-backed EncryptedData/SessionClose, not Error. Error uses
  `code:u16 LE || context:raw UTF-8`; nonempty Error is wire-incompatible with
  the historical TLV codec, without negotiation or fallback. See the
  [paired migration/rollback guide](../docs/guides/error-message-migration.md),
  including Rust's public u8 restriction; suite inventory is not interop proof.
  These files import only the public library and the standard library.
  `frame_test.go` owns the complete public frame layout,
  validation/error priority, receive-reserved-field and buffer-ownership suite;
  select it with `go test -run 'TestFrame|TestNewFrame' -count=1 ./integrationtests/api`.
  `messages_test.go` owns the complete typed-message golden, round-trip/ownership,
  truncation, text/UTF-8 and handshake-wrapper rejection suite, now enforced through
  the public import rather than root-package visibility.
  Concrete identity, method sets, current/nil error variables,
  validation priority, unchanged destinations, buffer ownership and allocation
  budgets are checked without access to private implementation seams. Header and
  control codec benchmarks live with these tests; use
  `go test -run '^$' -bench 'Benchmark(HeaderBoundary|ControlMessageBoundary)' -benchmem ./integrationtests/api`.
- `api/header_test.go`: complete public constructor/size/validation/error,
  literal wire layout and receive-reserved-field closure, with unchanged bodies.
- `api/tlv_decode_limits_test.go`: atomic malformed-suffix rejection, exact
  nil/owned shapes and caller/protocol/dense limits.
- `api/tlv_encode_regression_test.go`: public reference encoder, edge/error and
  copy-equivalence suite plus `BenchmarkEncodeTLVsPatch` and its sink.
- `api/tlv_decode_benchmark_test.go`: `BenchmarkDecodeTLVShapes`, including
  empty/tiny/dense/maximum/malformed public decoder workloads.
  These four complete closures require only the public library and stdlib;
  imports enforce external visibility without changing test bodies.
  Run `go test -run 'TestHeader|TestNewHeader|TestDecodeTLV|TestEncodeTLV' -count=1 ./integrationtests/api`
  and `go test -run '^$' -bench 'Benchmark(EncodeTLVsPatch|DecodeTLVShapes)' -benchmem -benchtime=5s ./integrationtests/api`.
- `connection_send_contract_test.go`, `connection_io_contract_test.go` and
  `server_admission_contract_test.go`: established runtime queue/write/cancellation,
  partial I/O and callback contracts; complete public fixtures stay in
  `runtime_helpers_test.go`.
- `tcp_test.go`: unchanged public-only oversized-header, EOF/malformed-frame and
  cancellation probes using `net.Pipe`.
- `tcp_stream_test.go`: fragmented/coalesced reads, canonical partial writes,
  truncation and serialized concurrent writes; `shortConn` and `framedBytes`
  remain with this complete stream closure.
- `tcp_cancellation_test.go`: lane cancellation and stale-deadline isolation,
  with `gatedIOConn` and `observingContext` synchronization fixtures.
  `tcp_helpers_test.go` contains public frame construction and bounded waits.
  Run `go test -count=1 -run 'TestTCPTransport|TestNewTCPTransport|TestHardeningTCP' ./integrationtests`.

Keep mutable-sentinel tests non-parallel: they intentionally replace public
variables, restore them on exit, and must not overlap other users in the same test
process. Keep the control and TLV-message files together: `messageSentinel` is a
shared local helper, not an exported testing API.

Root Server suites keep complete owners together: `server_handshake_test.go`
(establishment/allowlist/frame validation), `server_runtime_test.go` (slots,
reservation/atomic handoff snapshots and stress), `server_accept_test.go`
(classification/scripted listeners/backoff), `server_shutdown_test.go`
(Close/stalled handshake/admission socket). Config rejection stays in
`server_config_test.go`; shared keys/config live in `server_test_helpers_test.go`.

Root Connection suites follow behavioral ownership: `connection_keepalive_test.go`
for timers/Ping, `connection_receive_test.go` for dispatch/order/handler bounds,
`connection_lifecycle_test.go` for shutdown/first cause, and `connection_queue_test.go`
for queue capacity and disposal. `connection_test_helpers_test.go` retains shared
client/runtime fixtures; specialized start/close, cancellation and send/property
suites remain separate. Global scheduler and mutable-sentinel tests stay serial.

Root retains godoc examples, white-box session/runtime invariants, shared
`tcp_test_helpers_test.go` fixtures for white-box consumers and nil Connection
misuse in root `tcp_test.go`, vectors, conformance, fuzz targets and
other implementation-adjacent tests. Do not move a network-looking test merely
because of its name: injected dialers, private state and shared helpers are real
boundaries. Internal algorithm tests remain beside their stdlib-only leaves.
