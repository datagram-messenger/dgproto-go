# Independently runnable pinned echo peers

Unlike `go run ./examples/echo`, these commands run in separate processes and load
already provisioned identities through `dgproto.LoadStaticKey`. They do not generate,
export or persist private keys, discover pins, or implement trust-on-first-use.
They remain a bounded application example, not a production service or key manager.

## Provision inputs first

Use your application's trusted provisioning system to supply four files **outside
this checkout**:

| File | Exact contents | Reader |
|------|----------------|--------|
| `server.key` | 32 raw bytes of the server's X25519 private identity | Server only |
| `client.key` | 32 raw bytes of the client's X25519 private identity | Client only |
| `server.pub` | 32 raw bytes of the intended server's public key | Client pin |
| `client.pub` | 32 raw bytes of the authorized client's public key | Server allowlist |

These are binary files, not hex, base64, PEM, JSON or newline-terminated text.
The public files must correspond to the private identities. Existing application
code can derive the public bytes with `LoadStaticKey(privateBytes).Public()`; never
log private bytes or copy them into arguments, environment variables or this repository.
Obtain public pins through an authenticated channel, not from the server being tested.
Missing, zero, short and oversized files are rejected; reads are bounded to 33 bytes.
No fallback identities or empty allowlists are selected on an input failure.

Restrict private-file access with OS-appropriate permissions/ACLs and trusted parent
directories. The example does not audit filesystem permissions, prevent symlink races,
guarantee secret memory erasure, or provide encrypted-at-rest storage. Rotation and
trust updates are external, coordinated operations; never retry with an observed key
when a pin mismatch occurs. Automated tests use synthetic keys in temporary directories.

## Run from the repository root

In the server terminal (substitute your provisioned absolute paths):

```sh
go run ./examples/echo-server -listen 127.0.0.1:7443 -identity /secure/server.key -allowed-client /secure/client.pub
```

In a separate client terminal:

```sh
go run ./examples/echo-client -address 127.0.0.1:7443 -identity /secure/client.key -server-pin /secure/server.pub
```

On Windows use quoted native paths, for example `-identity "C:\secure\client.key"`.
For direct signal handling, build each command to an executable in an external temporary
folder and run that executable instead of the `go run` wrapper. Ctrl+C stops the server;
the client exits after one validated reply. Both binaries use the existing root module,
with no additional dependencies or nested module. Flags contain **file paths**, not secrets.

The server prints only `echo server listening`, without the address, peer identities,
keys or application payload. The client prints only `authenticated application echo received`
after validating the reply and joining cleanup. Errors are intentionally generic on stderr
with a nonzero exit status. Inspect trusted configuration and service availability yourself;
the commands do not dump underlying errors or supplied arguments. Required flags are
`-identity` and `-allowed-client` (server), or `-identity` and `-server-pin` (client).
The loopback address defaults to `127.0.0.1:7443`; avoid exposing this example publicly.

## Behavior and ownership

Application operation 1 requests the fixed `hello DGProto` TLV payload on stream 1;
operation 2 replies with an owned copy. Both sides validate the stream, operation,
exactly one type-1 field, and exact payload. This intentionally tiny schema is not a
multiplexed RPC layer. Unknown or malformed application envelopes fail the connection.
The protocol runtime handles its control messages. No payload-driven allocation beyond
the library's bounded frame and the validated fixed payload is introduced here.

The client installs its handler before explicit `Start`, bounds establishment to two
seconds and total runtime to five seconds, and waits for a separately validated
application reply. Successful dial or `SendPaddedAndWait` is not admission or delivery.
The request/reply remain immutable even when write-wait is canceled after enqueue.
Cancellation does not retract a request; no automatic retry, reconnect or idempotency
policy is provided. Padding of 16 bytes is authenticated cleartext, not obfuscation.

The client owns its connection from dial success, always closes it, then waits for
`Done` with an independent three-second cleanup context. The server owns its listener,
closes it even if cancellation precedes `Serve` registration, closes server-owned
connections and joins `Serve`. Its only handler performs a one-second bounded write;
there are no admission/disconnect hooks. `Server.Close` is not an overall shutdown
deadline, and a positive handler shutdown timeout does not kill detached goroutines.
Server limits here are four concurrent handshakes and eight active connections;
handshake timeout is two seconds, write and handler-shutdown budgets one second.
These are example-local choices, not changed library defaults or deployment budgets.

## Verify

```sh
go test -count=20 -timeout=120s ./examples/...
go test -race -count=1 ./examples/...
```

The shared example code is under `examples/internal/echoapp`, not the public library
API. Its tests exercise independent consumers, required input loading, wrong pins,
malformed replies, cancellation while awaiting an application reply, and listener
ownership on pre-canceled shutdown. See [the examples index](README.md) and
[the integration guide](../docs/guides/getting-started.md) for the underlying contracts.
