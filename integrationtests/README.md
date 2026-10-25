# Public integration tests

The six-file relocation preserved all 20 tests and two benchmarks byte-for-byte;
Git blob comparison and the 251-entry global test/benchmark/fuzz/example inventory
were unchanged. Local validation used Go 1.26.5 windows/amd64, CGO_ENABLED=1:
focused integration tests, vector/conformance selectors, full default/tagged tests
and vet, and full race passed on all seven packages. Race used process-local
MinGW PATH and GCC 16.2.0 (MSYS2 Rev4). Formatting/diff checks were empty;
DGPROTO_KEYLOG was unset. These are local results, not minimum-Go/other-OS,
fuzz-campaign, sustained-benchmark, vulnerability or interoperability evidence.

Run `go test -count=1 ./integrationtests/...` from the repository root. Plain
`go test` at root does not run these packages.

- `api/`: consumer contracts for Header, TLV, replay and typed handshake/control/
  application messages. All six files import only the public library and the
  standard library. Concrete identity, method sets, current/nil error variables,
  validation priority, unchanged destinations, buffer ownership and allocation
  budgets are checked without access to private implementation seams. Header and
  control codec benchmarks live with these tests; use
  `go test -run '^$' -bench 'Benchmark(HeaderBoundary|ControlMessageBoundary)' -benchmem ./integrationtests/api`.
- `tcp_test.go`: public-only oversized-header, EOF/malformed-frame and cancellation
  probes using `net.Pipe`.

Keep mutable-sentinel tests non-parallel: they intentionally replace public
variables, restore them on exit, and must not overlap other users in the same test
process. Keep the control and TLV-message files together: `messageSentinel` is a
shared local helper, not an exported testing API.

Root retains godoc examples, white-box session/runtime invariants, shared
transport helpers used by hardening tests, vectors, conformance, fuzz targets and
other implementation-adjacent tests. Do not move a network-looking test merely
because of its name: injected dialers, private state and shared helpers are real
boundaries. Internal algorithm tests remain beside their stdlib-only leaves.
