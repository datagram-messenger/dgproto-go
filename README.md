<div align="center">

<a href="https://github.com/datagram-messenger">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/datagram-logo-dark.svg" />
    <source media="(prefers-color-scheme: light)" srcset="docs/assets/datagram-logo-light.svg" />
    <img src="docs/assets/datagram-logo-dark.svg" width="96" height="96" alt="Datagram Messenger" />
  </picture>
</a>

# dgproto-go

**Authenticated, encrypted, bidirectional TCP sessions for Go.**

[![CI](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://github.com/datagram-messenger/dgproto-go/blob/main/go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](https://github.com/datagram-messenger/dgproto-go/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/datagram-messenger/dgproto-go)](https://github.com/datagram-messenger/dgproto-go/releases/latest)

</div>

Build two-way communication into your Go application without implementing framing, the Noise handshake, or session rekeying yourself. `dgproto-go` implements the **draft DGProto v1 strict MVP over TCP**, with client and server runtimes and lower-level protocol primitives.

Use it for desktop-client events, backend command channels, or custom binary protocols where either peer needs to send messages over a persistent connection. **It is a protocol/session library, not an application server:** you define accounts, permissions, application messages, and delivery guarantees.

- **Peer identity, not just encryption.** Noise XX authenticates static-key possession; the TCP client requires a provisioned server-key pin.
- **Session protection.** ChaCha20-Poly1305 data frames, replay checks, and directional rekeying managed by the connection runtime.
- **Explicit backpressure.** Bounded outbound and handler queues, plus separate server limits for handshakes and active connections.
- **A lifecycle you control.** Contexts, explicit client `Start`, shutdown APIs, and a retained terminal cause.
- **Structured binary messages.** Typed messages and TLV codecs, with application envelopes carrying stream IDs and message types.

> **Pre-1.0 / draft:** Wire compatibility is not yet stable. See [compatibility and upgrades](#compatibility-and-upgrades), including the **0.7.0 ErrorMessage wire break**.

[Quick start](#quick-start) · [Fit and limits](#fit-and-limits) · [Contracts](#contracts-worth-knowing) · [Documentation](#documentation)

## Install

Requires **Go 1.25.0 or newer**, with current security patches:

```sh
go get github.com/datagram-messenger/dgproto-go
```

## Quick start

### Try a pinned client/server exchange

Run a self-contained exchange on loopback — no separate service or pre-provisioned keys needed:

```sh
git clone https://github.com/datagram-messenger/dgproto-go.git
cd dgproto-go
go test -run "^ExampleDialContext$" -count=1 -timeout=15s -v
```

Already have a checkout? Run just the `go test` command from its root. A successful run reports `ExampleDialContext` as passing; the test checks that both peers exchanged an application envelope.

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

	establishmentCtx, stopEstablishment := context.WithTimeout(ctx, time.Second)
	conn, _, err := dgproto.DialContext(establishmentCtx, address, dgproto.ClientConfig{
		StaticKey:    clientKey,
		ServerStatic: serverPublic,
		ConnectionConfig: dgproto.ConnectionConfig{
			HandshakeTimeout: time.Second,
			WriteTimeout:     time.Second,
		},
	})
	stopEstablishment() // Does not cancel the established connection's lifetime.
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

Choose `dgproto-go` when you control both endpoints, can provision trusted peer keys, and want application-defined binary messages on a long-lived, two-way connection. Stream IDs and message types let you distinguish operations; your application supplies their semantics.

- **TCP, not independent parallel streams.** Handlers run serially per connection. Slow handlers delay later messages; a full handler queue terminates the connection. TCP head-of-line blocking still applies, regardless of stream IDs.
- **A fixed TCP profile.** Three-flight Noise XX and ChaCha20-Poly1305 only; no QUIC transport, cipher negotiation, 0-RTT, resumption, or traffic obfuscation. Random authenticated cleartext padding is available, with policy owned by the application.
- **Small, bounded frames.** The total wire frame is at most 65,535 bytes, including the 40-byte header, data-frame tag, and any padding. Application envelopes and TLVs reduce the available application payload further; chunk larger data yourself.
- **No automatic reconnect.** Reconnection and retry policy belong to the application.
- **Bounded defaults are a starting point.** Server defaults are queues of 16, 64 concurrent handshakes, and 1024 active connections. Handshake timeout defaults to 10 seconds; read/write/idle timeouts and keepalive are disabled by default. Set and test service-specific limits, downstream budgets, and [timeouts](docs/guides/getting-started.md#production-timeout-guidance) before deployment.

## Compatibility and upgrades

Go module releases and protocol draft versions are independent: the DGProto **v1** name does not promise stable wire compatibility. Match the exact draft and implementation revisions you deploy; the [test vectors](testdata/vectors/README.md) do not establish independent interoperability.

**0.7.0 breaks nonempty `ErrorMessage` wire compatibility:** context changed from historical TLV encoding to raw UTF-8, without negotiation or fallback. Upgrade and roll back both peer generations together. The [migration guide](docs/guides/error-message-migration.md) covers deployment steps, persisted payloads, and Rust's public error-code limit of 0–255.

## Security

The current profile uses `Noise_XX_25519_ChaChaPoly_SHA256` and ChaCha20-Poly1305 data-frame encryption. Authentication proves key possession, not account identity or permission. Applications own key storage and rotation, authorization, endpoint hardening, and dependency updates; neither the draft nor passing tests establishes production assurance.

Use the latest security patch of a [currently supported Go release](https://go.dev/doc/devel/release#policy), not just the minimum language version. Go 1.25 remains the module's language minimum, not an upstream security-support promise. CI retains a minimum-line compatibility check; release, race and vulnerability gates use Go 1.26.x. Earlier Windows fixes for [GO-2026-4971](https://pkg.go.dev/vuln/GO-2026-4971) landed in Go 1.25.10 and 1.26.3; those historical patch levels are not a current security baseline.

Optional traffic-secret logging requires both the `dgproto_keylog` build tag and `DGPROTO_KEYLOG` environment variable. Keep the tag out of production builds and the variable unset in ordinary runs; logged secrets expose captured traffic.

**Report vulnerabilities privately**, not in public issues: use the [GitHub security advisory form](https://github.com/datagram-messenger/dgproto-go/security/advisories/new). Read [SECURITY.md](SECURITY.md) for the policy.

## Documentation

| Start here | What you'll find |
|------------|------------------|
| [Getting started](docs/guides/getting-started.md) | Client/server integration, admission, ownership, shutdown, and timeouts |
| [Go API reference](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go) | Exported types, configuration, and method contracts |
| [Architecture](docs/architecture/overview.md) | Framing, rekey flow, replay handling, and concurrency |
| [Protocol specification](docs/protocol/dgproto-v1.md) | Normative draft wire format and cryptographic state transitions |
| [Test vectors](testdata/vectors/README.md) | Fixture scope and validation levels, not an interoperability certification |

## Contributing

Trying DGProto in a real workload? [Open an issue](https://github.com/datagram-messenger/dgproto-go/issues) with integration feedback, a documentation gap, or a public-safe bug report. Small examples of what was hard to integrate are especially useful. For code or documentation changes, start with the [contribution guide](.github/CONTRIBUTING.md).

## License

[Apache 2.0](LICENSE).
