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
go run ./examples/echo-server -listen 127.0.0.1:7443 -identity "/secure/server.key" -allowed-client "/secure/client.pub"
```

In a separate client terminal:

```sh
go run ./examples/echo-client -address 127.0.0.1:7443 -identity "/secure/client.key" -server-pin "/secure/server.pub"
```

For PowerShell, in separate terminals:

```powershell
go run ./examples/echo-server -listen 127.0.0.1:7443 -identity "C:\secure\server.key" -allowed-client "C:\secure\client.pub"
go run ./examples/echo-client -address 127.0.0.1:7443 -identity "C:\secure\client.key" -server-pin "C:\secure\server.pub"
```

`-h` / `-help` on either peer print curated usage and exit successfully without loading files or opening sockets. Help output failure is still an error; invalid input diagnostics remain generic and never echo paths. Parsing stops at help, so later arguments are not processed.
For direct signal handling, build each command to an executable in an external temporary
folder and run that executable instead of the `go run` wrapper. The binaries subscribe
to `os.Interrupt` (Ctrl+C in an attached console); the client exits after one validated
reply. Automated Windows smoke uses process kill, not console Ctrl+C, and does not
verify graceful console shutdown. SIGTERM is not explicitly subscribed here.
Both binaries use the existing root module, with no additional dependencies or nested module. Flags contain **file paths**, not secrets.

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
Outbound and handler queues each hold four pending messages; the runtime control
queue remains 16. A 15-second inbound idle timeout evicts silent peers. Inbound
activity resets that timer, so it is not an absolute connection lifetime. Read
timeout and keepalive remain disabled. These are example-local choices, not
changed library defaults, complete DoS protection or deployment budgets.

Start with [the learning index](README.md), then see the [client walkthrough](echo-client/README.md),
[server walkthrough](echo-server/README.md) and [shared policy](internal/echoapp/README.md).
If startup fails, check required nonempty file paths, raw file sizes and listener
availability. If exchange fails, check provisioned pins and allowlisting through
your trusted channel; never replace a mismatched pin with an observed network key.

## Verify

```sh
go test -short -count=20 -timeout=120s ./examples/...
go test -run 'TestSeparateProcessProvisionedEcho|TestExternalConsumerExampleCompiles' -count=3 -timeout=180s ./examples/echo-server
go test -race -count=1 -timeout=180s ./examples/...
```

The shared example code is under `examples/internal/echoapp`, not the public library
API. Its tests exercise independent consumers, required input loading, wrong pins,
malformed replies, cancellation/deadlines after request receipt, early peer termination,
and listener ownership on failed startup and canceled shutdown.

For a safe developer smoke, use the second command above (Windows cmd.exe: use double
quotes around the `-run` selector). It builds real server/client executables outside the
checkout, provisions fresh synthetic private files and locally derived public trust files
in `t.TempDir`, waits for exact readiness output, verifies success, rejects a wrong pin,
and checks a subsequent successful exchange. No user keys or observed pins are used;
`DGPROTO_KEYLOG` is removed from child environments. Captured output is bounded and
never dumped on failure. All child lifetimes and waits are bounded; cleanup kills only
the directly launched executable and joins its sole `Wait` before temporary-file cleanup.
On non-Windows hosts the test requests `os.Interrupt` and requires successful server
exit; Windows uses forced termination and listener release only. This is not evidence
of Windows Ctrl+C behavior or graceful shutdown under arbitrary OS signals.

The command has no inherited-listener interface: ephemeral port reservation followed
by command binding has a small local collision window. A collision fails readiness;
there are no sleep-based startup guesses or retries that change trust. `-short` skips
both build-heavy tests. Full `go test ./...` includes them. The external-consumer test
copies the public-only loopback example into a temporary module, uses a local `replace`,
and runs `go mod tidy` plus `go build`; it does not verify published-module availability.
Child builds use the selected Go toolchain without adding dependencies to this repository.
The parent race/tag gates do not implicitly instrument/tag binaries built by `go build`.
Temporary file modes are not a Windows ACL audit or a secure-erasure guarantee.

See [the examples index](README.md) and
[the integration guide](../docs/guides/getting-started.md) for the underlying contracts.
