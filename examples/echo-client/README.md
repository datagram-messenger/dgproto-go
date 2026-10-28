# Lesson 2: independent pinned client

This command sends one fixed application request to a separately running allowlisted server. Use a security-patched Go toolchain (minimum Go 1.25.0), repository-root working directory, and the four provisioned raw binary files described in [the canonical peer guide](../echo-pair.md). Start [the server](../echo-server/README.md) first.

```sh
go run ./examples/echo-client -help
go run ./examples/echo-client -address 127.0.0.1:7443 -identity "/secure/client.key" -server-pin "/secure/server.pub"
```

For PowerShell, substitute quoted native paths such as `"C:\secure\client.key"`. Flags are paths, never private bytes. `-identity` and `-server-pin` are required and nonempty; `-address` defaults to `127.0.0.1:7443`. Positional arguments, unknown flags, missing values and empty inputs fail without raw diagnostics. `-h` / `-help` print curated usage and exit 0 without key reads or dialing; output failure exits 1. An already canceled run returns cancellation without output.

Success prints exactly `authenticated application echo received` and exits 0 only after validated receipt and cleanup. Failure prints `echo client failed (check inputs, trust and server availability)` on stderr and exits 1; paths, keys, peer details and payloads are not logged. Ctrl+C requests cancellation via `os.Interrupt`.

## Source walkthrough

[main.go](main.go) handles safe flags, loads identity and trusted public pin, calls `Exchange`, then writes success. [identity.go](../internal/echoapp/identity.go) rejects invalid raw inputs with a 33-byte read cap; that is not a time limit, permissions audit or protection against special files/symlink races. Trust must be provisioned externally; no TOFU, key export, retry or reconnect is added.

[client.go](../internal/echoapp/client.go) owns the stopped connection from successful dial. It installs the callback before explicit `Start`, cancels the two-second establishment context independently, and bounds runtime to five seconds. The one-second write-wait is local completion only. A separate callback validates stream 1, reply operation 2 and exactly one type-1 `hello DGProto` field. Request operation is 1; padding is 16 authenticated cleartext bytes. Construction copies fields; neither preflight nor queue admission freezes buffers. Messages stay immutable even after cancellation.

The server remains open after replying so shutdown cannot discard the client's queued callback prematurely. Cleanup closes and joins `Done` with an independent three-second wait, without claiming callback draining. Exact benign close sentinels are ignored only after validated success; wrapping or joining preserves error information. Client queues retain defaults of 16, with read/idle timeouts disabled. This is a bounded educational exchange, not a persistent client or deployment policy.

## Verify and continue

```sh
go test -count=20 -timeout=120s ./examples/echo-client
go test -race -count=1 ./examples/echo-client
```

Tests cover synthetic provisioned round trip, output errors, safe help, invalid options and pre-canceled input loading. Shared tests cover wrong pins, malformed replies, cancellation and peer termination. Next: [server ownership](../echo-server/README.md), [shared policy](../internal/echoapp/README.md), and [godoc lessons](../README.md#learning-path).
