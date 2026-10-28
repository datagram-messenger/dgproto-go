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
  Contract tests import only the public library and the standard library.
  The comparison benchmark in `api/benchmark_test.go` additionally imports
  `golang.org/x/crypto/chacha20poly1305`, an existing module dependency; it is
  a synthetic in-memory comparison, not a network or TLS benchmark.
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
- `api/encrypted_data_size_test.go`: EncodedLen/ValidateFrameSize wire length,
  ownership, validation/error priority, current/nil sentinels, representative
  single-field crypto boundaries and zero-allocation valid calls.
- `api/encrypted_data_size_all_padding_test.go`: all 256 padding budgets with
  multiple independently aligned TLVs, last-fitting/first-oversized aggregate
  limits, envelope-before-frame rejection and Codec encryption/roundtrip parity.
- `api/encrypted_data_size_benchmark_test.go`: same-input preflight versus
  MarshalBinary comparisons, not a before/after encoder optimization claim.
  `api/encrypted_data_size_example_test.go` contains the ValidateFrameSize
  example source; root `encrypted_data_size_example_test.go` contains EncodedLen.
  Both are runnable output examples. Keep each trailing Output directive in its
  own comment group, separate from explanatory comments, for Go discovery.
  Verify registration with `go test -list ExampleEncryptedData ./...`; the
  selector below runs both size tests and example output checks.
  Implementation remains in root `messages_tlv.go`; preflight is optional,
  does not snapshot/reserve inputs or capacity, and is not performed by Send APIs.
  Select with `go test -run 'TestEncryptedData(EncodedLen|Size|ValidateFrameSize)|ExampleEncryptedData' -count=1 ./...`;
  benchmark with `go test -run '^$' -bench BenchmarkEncryptedDataSizePreflight -benchmem ./integrationtests/api`.
- `api/connection_constructor_test.go`: public nil-dependency constructor panics.
- `api/rekey_state_test.go`: public RekeyState derivation, confirmation and
  validation contracts; this does not replace root Session transaction tests.
- `api/messages_extended_test.go` and `api/tlv_test.go`: public extended-message
  and TLV contracts with their complete local helper closures.
- `api/keylog_disabled_test.go`: default-build no-write behavior. Tagged sink
  tests stay in `internal/keylog/`; root Session integration uses subprocesses.
- `api/benchmark_test.go`: public frame/message and synthetic envelope comparison
  benchmarks. Select with `go test -run "^$" -bench . -benchmem ./integrationtests/api`.
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
- `tcp_duplex_cancellation_test.go`: simultaneous active read/write I/O proves
  cancellation deadlines are direction-local, reusing the existing gates.
  `tcp_helpers_test.go` contains public frame construction and bounded waits.
  Run `go test -count=1 -run 'TestTCPTransport|TestNewTCPTransport|TestHardeningTCP|TestTCPDuplex' ./integrationtests`.

Keep mutable-sentinel tests non-parallel: they intentionally replace public
variables, restore them on exit, and must not overlap other users in the same test
process. Keep the control and TLV-message files together: `messageSentinel` is a
shared local helper, not an exported testing API.

`server_stats_test.go` owns public constructed-default/custom-limit and gated
resource snapshot lifecycle/concurrent Serve/shutdown contracts, reusing the
`lifecycleListener`, real Noise helper and bounded waits from the runtime suite.
`server_admission_contract_test.go` also checks snapshot cleanup after admission
rejection and admission/disconnect panic recovery. Root `server_stats.go` owns
this secret-free advisory API; root `server_stats_test.go` checks coherent map
handoff (including before Start), and `server_stats_example_test.go` retains the
executable godoc example. Pending/active ownership and shutdown are sampled
under one mutex; slot gauges are independently observed, not a globally atomic
snapshot. A handshake slot is released before Admission, while a connection
slot can remain occupied through OnDisconnect after active ownership ends.
Active counts handoff, not live/ready peers; Stats reserves no capacity.
Select with
`go test -run 'TestServerStats|TestServerAdmissionDisconnect|ExampleServer_Stats' -count=1 ./...`.

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
suites remain separate. `connection_rekey_boundary_test.go` adds gated caller/runtime
cancellation and zero/partial-write failures before and after the send-key commit;
its trigger and commit snapshots require root-private state. Global scheduler
and mutable-sentinel tests stay serial.

Root retains godoc examples, white-box session/runtime invariants, shared
`tcp_test_helpers_test.go` fixtures for white-box consumers, vectors,
conformance, fuzz targets and
other implementation-adjacent tests. Do not move a network-looking test merely
because of its name: injected dialers, private state and shared helpers are real
boundaries. Internal algorithm tests remain beside their stdlib-only leaves.
