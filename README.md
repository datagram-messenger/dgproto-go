<div align="center">

<a href="https://github.com/datagram-messenger"><img src="docs/assets/datagram-logo-dark.svg" width="96" height="96" alt="Datagram Messenger" /></a>

# dgproto-go

**Authenticated, encrypted, bidirectional TCP sessions for Go.**

[![CI](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://github.com/datagram-messenger/dgproto-go/blob/main/go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](https://github.com/datagram-messenger/dgproto-go/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/datagram-messenger/dgproto-go)](https://github.com/datagram-messenger/dgproto-go/releases/latest)

</div>

`dgproto-go` gives your Go application a binary session layer for two-way communication: establish a peer identity, exchange encrypted messages, and manage the connection's lifetime. It implements **DGProto v1 over TCP** for native clients and Go backends.

Use it for desktop-client events, backend command channels, or custom protocol workloads where both peers send messages over a persistent connection. Go applications can use the client and server APIs; native Rust clients can target the same draft protocol, with compatibility validated against the exact revisions in use. **This is a transport/session library, not an application server:** accounts, permissions, payload semantics, and durable delivery remain yours.

- **Peer identity, not just encryption.** Noise XX authenticates static-key possession; the TCP client requires a provisioned server-key pin.
- **Session protection.** ChaCha20-Poly1305 data frames, replay checks, and directional rekeying managed by the connection runtime.
- **Explicit backpressure.** Bounded outbound and handler queues, plus separate server limits for handshakes and active connections.
- **A lifecycle you control.** Contexts, explicit client `Start`, shutdown APIs, and a retained terminal cause.
- **Structured binary messages.** Typed messages and TLV codecs, with application envelopes carrying stream IDs and message types.

> **Status:** Pre-1.0 Go module; DGProto v1 is a draft. Module and protocol versions are independent, and wire compatibility is not yet a stable guarantee.

## Install

Requires **Go 1.25+**:

```sh
go get github.com/datagram-messenger/dgproto-go
```

Use a security-patched Go toolchain. Windows TCP clients require Go 1.25.10 or a later patched release to address the standard-library advisory [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971); the module's minimum language version is not a security-patch guarantee.

## Quick start

### Try a pinned client/server exchange

From a checkout of this repository:

```sh
go test -run "^ExampleDialContext$" -count=1 -v
```

The [complete executable example](client_example_test.go) creates loopback peers, allowlists the client key, pins the server key, calls `Start`, exchanges application envelopes, and closes both sides. It uses ephemeral in-memory identities and bounded waits — **a learning example, not production key management or an application server**.

### Integrate a pinned client

This small integration helper is not a standalone program. Supply an existing server address, a securely provisioned client key, and the intended server's 32-byte public-key pin. It sends one application envelope and closes the connection; a persistent client should keep the runtime open and install a receive handler.

```go
package example

import (
	"context"
	"errors"
	"time"

	dgproto "github.com/datagram-messenger/dgproto-go"
)

// WriteEnvelope reports a local socket write, not peer receipt or admission.
func WriteEnvelope(ctx context.Context, address string, clientKey dgproto.StaticKey,
	serverPublic []byte, message dgproto.EncryptedData) (err error) {
	runtimeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, _, err := dgproto.DialContext(runtimeCtx, address, dgproto.ClientConfig{
		StaticKey:    clientKey,
		ServerStatic: serverPublic,
		ConnectionConfig: dgproto.ConnectionConfig{
			HandshakeTimeout: time.Second,
			WriteTimeout:     time.Second,
		},
	})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()

	conn.Start(runtimeCtx) // DialContext returns a stopped, caller-owned runtime.
	return conn.SendAndWait(runtimeCtx, message)
}
```

The timeouts above are illustrative, not universal service budgets. Keep the message and its reachable buffers immutable while pending: a canceled write-wait does not retract an accepted message.

**Server integration:** construct `NewServer(ServerConfig{...})`, then pass a TCP listener to `Serve`. Configure `AllowedClients` and/or `Admission` before dispatch: an empty allowlist permits any valid authenticated client, and a successful client dial does **not** prove server admission or application authorization. Persist and protect static identities; provision and rotate pins through a trusted channel.

Continue with [server setup](docs/guides/getting-started.md#server), [admission and authorization](docs/guides/getting-started.md#admission-and-authorization), and [lifecycle](docs/guides/getting-started.md#lifecycle).

## Contracts worth knowing

| API | Success means |
|-----|---------------|
| `Send` / `TrySend` | Nonblocking queue acceptance; saturation returns `ErrOutboundQueueFull` |
| `SendContext` | Queue acceptance after waiting for capacity, not network I/O |
| `SendAndWait` | Complete **local** transport write, including any required rekey |

The padded variants follow the same acceptance/write distinction. **None promises peer receipt, processing, acknowledgement, or durable delivery.** Define application acknowledgements, retries, and idempotency explicitly.

- **Runtime ownership:** call `Start(runtimeCtx)` and always close a dialed connection, even if it never starts. The dial context bounds establishment only; canceling it after success does not stop the runtime.
- **Shutdown:** `Connection.Close` attempts a close-frame write, not a queue drain or peer-acknowledged close. `Done` tracks runtime completion; `Err` retains the first terminal cause.
- **Server shutdown:** `Close` waits for server-owned work. A positive `HandlerShutdownTimeout` detaches a stuck canceled handler; zero can wait indefinitely. `Abort` closes networking without waiting for callbacks, but cannot kill their goroutines. Bound handler and hook work yourself.

See [sending and ownership](docs/guides/getting-started.md#sending-and-ownership) and the [Go API reference](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go) for the full contracts, including lower-level session/rekey APIs.

## Fit and limits

- **TCP, not independent parallel streams.** Handlers run serially per connection. Slow handlers delay later messages; a full handler queue terminates the connection. TCP head-of-line blocking still applies, regardless of stream IDs.
- **No automatic reconnect.** Reconnection and retry policy belong to the application. The current MVP has no cipher negotiation, 0-RTT, or resumption.
- **Bounded defaults are a starting point.** Server defaults are queues of 16, 64 concurrent handshakes, and 1024 active connections. Handshake timeout defaults to 10 seconds; read/write/idle timeouts and keepalive are disabled by default. Set and test service-specific limits, downstream budgets, and [timeouts](docs/guides/getting-started.md#production-timeout-guidance) before deployment.

## Security

The current profile uses `Noise_XX_25519_ChaChaPoly_SHA256` and ChaCha20-Poly1305 data-frame encryption. Authentication proves key possession, not account identity or permission. Applications own key storage and rotation, authorization, endpoint hardening, and dependency updates; neither the draft nor passing tests establishes production assurance.

**Report vulnerabilities privately**, not in public issues: use the [GitHub security advisory form](https://github.com/datagram-messenger/dgproto-go/security/advisories/new). Read [SECURITY.md](SECURITY.md) for the policy.

## Documentation

| Start here | What you'll find |
|------------|------------------|
| [Getting started](docs/guides/getting-started.md) | Client/server integration, admission, ownership, shutdown, and timeouts |
| [Go API reference](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go) | Exported types, configuration, and method contracts |
| [Architecture](docs/architecture/overview.md) | Framing, rekey flow, replay handling, and concurrency |
| [Protocol specification](docs/protocol/dgproto-v1.md) | Normative draft wire format and cryptographic state transitions |
| [Test vectors](testdata/vectors/README.md) | Fixture scope and validation levels, not an interoperability certification |
| [0.6.0 hardening notes](docs/0.6.0-candidate.md) | Release scope, recorded validation, and remaining gates |

## Contributing

Trying DGProto in a real workload? [Open an issue](https://github.com/datagram-messenger/dgproto-go/issues) with integration feedback, a documentation gap, or a public-safe bug report. Small examples of what was hard to integrate are especially useful. For code or documentation changes, start with the [contribution guide](.github/CONTRIBUTING.md).

## License

[Apache 2.0](LICENSE).
