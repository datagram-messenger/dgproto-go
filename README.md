# dgproto-go

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
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
