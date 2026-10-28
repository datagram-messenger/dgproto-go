# Shared peer policy (example-local)

This internal package supports [the client](../../echo-client/README.md) and [the server](../../echo-server/README.md) in the existing root module. It has no executable, dependencies of its own or supported public consumer contract. Begin with [loopback](../../echo/README.md), then [provision the independent peers](../../echo-pair.md). Run commands and tests from the repository root with patched Go (minimum 1.25.0).

## Responsibility map

| File | Responsibility |
| --- | --- |
| [identity.go](identity.go) | Load exact raw 32-byte identity/public files; generic input errors, no fallback trust |
| [envelope.go](envelope.go) | Named fixed schema: stream 1, request 1, reply 2, one type-1 field with `hello DGProto`; 16-byte padding and one-second operation budget |
| [client.go](client.go) | Two-second establishment, five-second runtime, installed callback before Start, validated reply, caller-owned close and Done join |
| [server.go](server.go) | Explicit copied allowlist, bounded reply write, listener/Serve ownership and shutdown |

Identity files and pins must be provisioned through a trusted channel. The 33-byte read cap limits bytes, not time; it does not audit ACLs, symlinks, special files or memory erasure. Do not log identities or downgrade a mismatched pin. No key generation/export framework, TOFU, retries or reconnect is provided.

`NewEncryptedData` copies source fields but the returned buffers remain mutable. Preflight is not a reservation; send queues do not snapshot. Keep messages immutable even after canceled write-wait. Local write is not receipt. The server stays open after replying; the client closes only after callback validation or failure/cancellation. Close and Done do not drain callbacks. Cleanup ignores only exact benign close sentinels after successful validation, preserving wrapped/joined information.

The independent server allows four handshakes and eight connections, with two-second handshake and one-second write/handler-shutdown budgets. Outbound and handler queues each hold four pending messages (the runtime control queue remains 16). A 15-second inbound idle timeout evicts silent peers but resets on inbound activity: it is not an absolute connection deadline. Read timeout and keepalive remain disabled; library defaults are unchanged. Service-level admission, authorization and liveness budgets remain application responsibilities. Cleanup joins use independent three-second contexts; Server.Close itself is not an overall deadline, and detaching a stuck callback does not kill it.

## Tests and next lesson

```sh
go test -short -count=20 -timeout=120s ./examples/...
go test -race -count=1 -timeout=180s ./examples/...
```

`exchange_test.go` owns schema/round-trip tests; `failure_test.go` and `termination_test.go` cover malformed replies, cancellation and early peer close; `identity_test.go` checks file and trust inputs; `client_test.go` preserves exact-versus-composite close causes; `server_test.go` exercises real idle eviction (skipped by `-short` to avoid repeating the 15-second budget). Command-local tests cover flags/output/listener startup. The server command retains `consumer_test.go` and `subprocess_test.go` because they validate command-level consumers, not an exported helper API. Child builds are ordinary uninstrumented/untagged builds unless explicitly configured.

The loopback main deliberately duplicates a small policy using only public root imports, keeping it independently copyable. Next: [executable godoc lessons](../../README.md#learning-path), especially constructor ownership, size validation, client context separation and advisory Server.Stats.
