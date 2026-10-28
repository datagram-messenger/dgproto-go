# Runnable examples

Run these commands from the repository root with a supported, security-patched Go toolchain (module minimum: Go 1.25.0). Examples use the root module; there is no separate `go.mod`, service, configuration file, or new dependency.

| Example | Run | Demonstrates |
|---------|-----|--------------|
| [Pinned loopback echo](echo/main.go) | `go run ./examples/echo` | Allowlisting, a trusted server pin, explicit client startup, TLV payload validation, padded write-wait, and shutdown/join |

Successful echo output is exactly:

```text
authenticated application echo received
```

The command opens `127.0.0.1:0`, generates ephemeral server/client static identities in memory, allowlists only that client, and supplies the server's public key directly to the client. It sends one immutable application envelope and validates the echoed stream, operation, field type, and payload before reporting success. It prints no keys, peer metadata, or payloads; failure produces `echo failed` on stderr and a nonzero exit status. Ctrl+C cancels the run.

Establishment is bounded to two seconds, runtime to five seconds, and each write/callback to one second. These are illustrative loopback budgets, not production defaults. The client handler is configured before `Start`; canceling establishment after a successful `DialContext` does not cancel runtime. Cleanup closes the client, waits for `Done`, closes the server (joining its bounded callbacks), closes the owned listener, and joins `Serve`. Additional join waits use independent three-second contexts, so cleanup is not skipped when runtime expires. `Server.Close` itself is not an overall deadline; this example has no unbounded hooks.

`SendPaddedAndWait` confirms only a local write. The separate validated application echo establishes this demo's receipt check, not a general acknowledgement or durable delivery contract. Buffers remain immutable even on canceled sends. Padding is authenticated cleartext, not obfuscation; `ValidateFrameSize` is optional preflight, not a reservation or snapshot.

**Learning only:** ephemeral keys are not production provisioning or persistence. Real applications must protect and rotate identities/pins through a trusted channel, authorize operations, validate their own payloads, and choose service-specific limits. This is not an application server, reconnect policy, or trust-on-first-use workflow.

## Tests and further reading

```sh
go test -count=20 -timeout=120s ./examples/echo
go test -race -count=1 ./examples/echo
```

Tests cover a bounded round trip, already canceled/expired contexts, and rejection of malformed application echoes. Full `go test ./...` discovers them automatically.

The root godoc examples remain available: [DialContext](../client_example_test.go), [EncodedLen](../encrypted_data_size_example_test.go), and [Server.Stats](../server_stats_example_test.go). See [getting started](../docs/guides/getting-started.md) for integration, ownership and lifecycle contracts, and [the main README](../README.md) for scope and security guidance.
