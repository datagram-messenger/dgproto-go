# Lesson 3: independent allowlisted server

This command accepts provisioned peers and validates a tiny fixed application schema. From the repository root, use a security-patched Go toolchain (minimum Go 1.25.0). Follow [the canonical provisioning guide](../echo-pair.md) first; neither this command nor the client generates/exports identity files.

```sh
go run ./examples/echo-server -help
go run ./examples/echo-server -listen 127.0.0.1:7443 -identity "/secure/server.key" -allowed-client "/secure/client.pub"
```

PowerShell accepts the same command with quoted native file paths. `-identity` and `-allowed-client` are required and nonempty; `-listen` defaults to `127.0.0.1:7443`. Avoid public exposure. `-h` / `-help` print curated usage and exit 0 without reading files or binding a listener. Unknown/positional/missing/empty options fail generically, never echoing supplied input. Help output errors also fail. An already canceled run emits nothing.

Readiness is exactly `echo server listening`, printed after binding but before `Serve` begins; it is not client admission or application readiness. The server keeps serving after a client completes. Ctrl+C (`os.Interrupt`) closes server-owned work, releases the listener and joins `Serve`; normal requested shutdown exits 0. Failures print `echo server failed (check inputs and listener availability)` on stderr and exit 1. No addresses, identities, secrets or payloads are printed. SIGTERM is not explicitly subscribed. Windows subprocess tests kill the executable and prove listener release only, not graceful Ctrl+C behavior.

## Source walkthrough and policy

[main.go](main.go) parses safe flags, loads trusted raw inputs, creates the server and listener, writes readiness, and transfers ownership to `Serve`. Failed startup releases resources. [server.go](../internal/echoapp/server.go) refuses an empty/all-zero allowlist, copies the single authorized public key, and leaves operations to the fixed `Reply` callback. Noise authenticates key possession, not an account or permission; extra authorization is application policy.

`Reply` requires stream 1, operation 1 and exactly one type-1 `hello DGProto` field. It constructs an owned operation-2 reply, preflights 16 bytes of authenticated cleartext padding and waits at most one second for local write. The server stays open after writing until the client validates and closes; immediate close can discard a queued receive callback. Never mutate pending buffers, including after cancellation. `Close`/`Done` are not drains or receipt acknowledgements.

Example limits are four concurrent handshakes and eight active connections, with two-second handshake and one-second write/handler-shutdown budgets. Outbound and handler queues each hold four pending messages; the runtime control queue retains its library bound of 16. A 15-second inbound idle timeout evicts silent peers; inbound activity resets it, so it is not an absolute connection lifetime. Read timeout and keepalive remain disabled. These example-only limits do not change library defaults or provide complete DoS defense; choose and test service budgets separately. The handler has bounded writes and no unbounded admission/disconnect hooks. `Server.Close` is not an overall deadline; positive handler shutdown timeout detaches rather than kills stuck work. Listener and Serve cleanup use independent waits so canceled runtime does not skip ownership cleanup.

## Verify and continue

```sh
go test -short -count=20 -timeout=120s ./examples/...
go test -run 'TestSeparateProcessProvisionedEcho|TestExternalConsumerExampleCompiles' -count=3 -timeout=180s ./examples/echo-server
go test -race -count=1 -timeout=180s ./examples/...
```

Use double quotes around selectors in cmd.exe. Tests cover startup errors, listener release, cancellation, safe help, provisioned independent processes, wrong pins and the standalone public-only loopback consumer. Parent race/tag tests do not instrument/tag child binaries built with ordinary `go build`. See [shared policy and test ownership](../internal/echoapp/README.md), then [executable godoc features](../README.md#learning-path).
