# Runnable examples

Run these commands from the repository root with a supported, security-patched Go toolchain (module minimum: Go 1.25.0). Examples use the root module with no nested `go.mod` or new dependency. The loopback command is self-contained; the independent peers require provisioned key files.

| Example | Run | Demonstrates |
|---------|-----|--------------|
| [Pinned loopback echo](echo/README.md) | `go run ./examples/echo` | Zero-setup, copyable public-API exchange with ephemeral identities |
| [Independent server](echo-server/README.md) | `go run ./examples/echo-server -help` | Provisioned identity, single-client allowlist, listener ownership and shutdown |
| [Independent client](echo-client/README.md) | `go run ./examples/echo-client -help` | Provisioned identity and trusted pin, bounded request and reply validation |

## Learning path

1. Start with [loopback echo](echo/README.md); read its public-only `main.go` from setup through cleanup.
2. Follow the canonical [peer provisioning guide](echo-pair.md), then the client and server walkthroughs. The [shared implementation](internal/echoapp/README.md) separates identity input, schema, client lifetime and server ownership within one example-local package.
3. Read the executable godoc lessons: [DialContext and DialAndStartContext](../client_example_test.go), [NewEncryptedData](../encrypted_data_builder_test.go), [EncodedLen](../encrypted_data_size_example_test.go), [ValidateFrameSize](../integrationtests/api/encrypted_data_size_example_test.go), and [Server.Stats](../server_stats_example_test.go). They exercise public features without redundant commands.

```sh
go test -list '^Example' . ./integrationtests/api
go test -run '^Example' -count=1 -v . ./integrationtests/api
```

For Windows cmd.exe, use double quotes around test selectors.

For separate terminals with provisioned identities, see [independent pinned peers](echo-pair.md): `go run ./examples/echo-server` and `go run ./examples/echo-client` require explicit private identity and trusted public-key file flags. Their input, trust and lifecycle contracts are documented there; neither command exports secrets or adopts observed keys.

Successful loopback echo output is exactly:

```text
authenticated application echo received
```

The command opens `127.0.0.1:0`, generates ephemeral server/client static identities in memory, allowlists only that client, and supplies the server's public key directly to the client. It sends one immutable application envelope and validates the echoed stream, operation, field type, and payload before reporting success. It prints no keys, peer metadata, or payloads; failure produces `echo failed` on stderr and a nonzero exit status. Ctrl+C cancels the run.

Establishment is bounded to two seconds, runtime to five seconds, and each write/callback to one second. These are illustrative loopback budgets, not production defaults. The client handler is configured before `Start`; canceling establishment after a successful `DialContext` does not cancel runtime. Cleanup closes the client, waits for `Done`, closes the server (joining its bounded callbacks), closes the owned listener, and joins `Serve`. Additional join waits use independent three-second contexts, so cleanup is not skipped when runtime expires. `Server.Close` itself is not an overall deadline; this example has no unbounded hooks.

Both standard demos coordinate termination: the server remains open after writing its reply; the client initiates close only after its callback validates and publishes that reply (or after failure/cancellation). No application ack is needed for this client-owned close order. An arbitrary peer that closes immediately after its local reply write can trigger runtime shutdown before the queued callback starts. Queued callbacks are dropped, not drained; `Exchange` then returns a terminal error, not promised success. Preference for `received` over `Done` covers only an already published result, not future callback execution.

`SendPaddedAndWait` confirms only a local write. The separate validated application echo establishes this demo's receipt check, not a general acknowledgement or durable delivery contract. Buffers remain immutable even on canceled sends. Padding is authenticated cleartext, not obfuscation; `ValidateFrameSize` is optional preflight, not a reservation or snapshot.

**Learning only:** ephemeral keys are not production provisioning or persistence. Real applications must protect and rotate identities/pins through a trusted channel, authorize operations, validate their own payloads, and choose service-specific limits. This is not an application server, reconnect policy, or trust-on-first-use workflow.

## Tests and further reading

```sh
go test -count=20 -timeout=120s ./examples/echo
go test -race -count=1 ./examples/echo
```

Tests cover a bounded round trip, already canceled/expired contexts, and rejection of malformed application echoes. Full `go test ./...` discovers them automatically.

The root godoc examples remain available: [DialContext and DialAndStartContext](../client_example_test.go), [NewEncryptedData](../encrypted_data_builder_test.go), [EncodedLen](../encrypted_data_size_example_test.go), and [Server.Stats](../server_stats_example_test.go). Run the new convenience examples with `go test -run 'ExampleDialAndStartContext|ExampleNewEncryptedData' -count=1 -timeout=15s .`. The connect-and-start helper keeps establishment and runtime lifetimes independent; handlers are configured before start and may run before return. The owned-envelope constructor copies reusable input fields and values; the returned envelope must still remain immutable while queued. The runnable echo request/reply now uses that constructor instead of manual field cloning. See [getting started](../docs/guides/getting-started.md) for integration, ownership and lifecycle contracts, and [the main README](../README.md) for scope and security guidance.
