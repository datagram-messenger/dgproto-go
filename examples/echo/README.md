# Lesson 1: pinned loopback echo

Run from the repository root with a security-patched Go toolchain (minimum Go 1.25.0). No files, credentials, separate service or nested module are needed.

```sh
go run ./examples/echo
```

Exit 0 prints exactly `authenticated application echo received`. Failure prints `echo failed` on stderr and exits 1. This command has no flags; Ctrl+C cancels via `os.Interrupt`.

## Walk through main.go

1. `exchange` rejects an already canceled context before keys or networking. It generates ephemeral identities, binds only `127.0.0.1:0`, allowlists its one client and supplies the server public key directly as the trusted pin. No observed network key becomes trusted.
2. Server and client handlers check the entire fixed schema: stream 1, request operation 1 / reply operation 2, exactly one type-1 field containing `hello DGProto`. Control messages belong to the runtime.
3. `NewEncryptedData` owns copied fields. `ValidateFrameSize(16)` checks the padded wire budget; `SendPaddedAndWait` waits for the local write, not peer processing. Padding is authenticated cleartext, not obfuscation. Keep the resulting message and reachable buffers immutable even after canceled write-wait.
4. Dial establishment has a two-second context, separate from the five-second runtime. The handler is installed before `Start`; canceling establishment after success does not stop runtime. Successful dial does not guarantee server admission.
5. `waitEcho` requires a callback-published result. The server stays open after reply write until the client validates and closes. `Done` and `Close` do not drain callbacks. Only exact benign close sentinels after successful validation are suppressed; wrapped/joined causes survive.
6. Cleanup closes the caller-owned client, waits for `Done`, closes server-owned work and the listener, and joins `Serve` using independent three-second waits. Write and handler-shutdown budgets are one second; server limits are one handshake and one active connection. Server queues retain library defaults of 16; read/idle timeouts are disabled. These are learning budgets, not DoS protection or an overall shutdown deadline.

[main.go](main.go) intentionally duplicates a small schema and exchange rather than importing example helpers: it can be copied into an external consumer using only the public root import. The external-consumer test keeps that contract executable. Ephemeral identities are not persistence, rotation or deployment provisioning.

## Verify and continue

```sh
go test -count=20 -timeout=120s ./examples/echo
go test -race -count=1 ./examples/echo
```

Tests cover round trip, malformed envelopes, cancellation, output failure, terminal-result races and exact-versus-composite close causes. Next: [provision independent peers](../echo-pair.md), then [the client lesson](../echo-client/README.md). Return to [the examples index](../README.md) for executable godoc lessons.
