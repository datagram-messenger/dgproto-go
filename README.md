<div align="center">

# dgproto-go

**Go implementation of the DGProto v1 wire protocol and secure session runtime.**

[![CI](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/datagram-messenger/dgproto-go/actions/workflows/ci.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/datagram-messenger/dgproto-go.svg)](https://pkg.go.dev/github.com/datagram-messenger/dgproto-go)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://github.com/datagram-messenger/dgproto-go/blob/main/go.mod)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache--2.0-blue.svg)](https://github.com/datagram-messenger/dgproto-go/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/datagram-messenger/dgproto-go)](https://github.com/datagram-messenger/dgproto-go/releases/latest)

</div>

Go 1.25+ implementation of the draft DGProto v1 wire protocol and secure session runtime. It is a protocol library, not an application server.

- **L0:** TCP length-delimited transport
- **L1:** fixed header and strict frame parsing
- **L2:** Noise XX and ChaCha20-Poly1305 protection
- **L3:** epochs, rekeying, replay protection, keepalive, and lifecycle
- **L4:** typed messages and application dispatch

The [DGProto v1 specification](docs/protocol/dgproto-v1.md) is normative for wire behavior. The draft protocol and Go module releases are versioned independently.

## Install

```sh
go get github.com/datagram-messenger/dgproto-go
```

## Server setup

`ServerConfig.AllowedClients` is an optional authenticated-static-key allowlist. `Admission` runs after the Noise handshake and is the application authorization boundary. `OnDisconnect` receives the connection terminal result. Zero values select a 10-second server handshake timeout, queue capacities of 16, a limit of 64 concurrent handshakes, and 1024 established connections.

See [Getting started](docs/guides/getting-started.md) for a compile-checked server example and shutdown guidance.

## Sending and lifecycle

- `Send` reports acceptance into the bounded outbound queue, not a socket write, peer receipt, or acknowledgement.
- `SendAndWait` waits for the local writer result; success means the frame was written locally, not acknowledged by the peer or application.
- The first observed terminal cause is retained. `Abort` terminates a connection immediately; server shutdown is explicit through `Close` or immediate through `Abort`.
- `SendPadded` requests explicit zero padding. Choosing padding lengths and policy is an application responsibility.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/guides/getting-started.md)
- [Architecture](docs/architecture/overview.md)
- [Normative protocol](docs/protocol/dgproto-v1.md)
- [Test vectors](testdata/vectors/README.md)
- [Contributing](.github/CONTRIBUTING.md)
- [Security](SECURITY.md)
