# Getting Started with DGProto v1

This guide walks you through importing the package, setting up a server, and
connecting a client — all in Go.

## Prerequisites

- Go 1.25 or later
- A module that imports `github.com/datagram-messenger/dgproto-go`

```bash
go get github.com/datagram-messenger/dgproto-go
```

## 1. Generate Static Key Pairs

Both peers need a long-term X25519 static key. Generate one (or load an
existing one from persistent storage):

```go
import dgproto "github.com/datagram-messenger/dgproto-go"

staticKey, err := dgproto.GenerateStaticKey()
if err != nil {
    log.Fatal(err)
}
// Persist the private key material securely in your application's key store.
// Use LoadStaticKey to restore it on the next start.
```

## 2. Start a Server

```go
package main

import (
    "context"
    "errors"
    "log"
    "net"

    dgproto "github.com/datagram-messenger/dgproto-go"
)

func main() {
    staticKey, err := dgproto.GenerateStaticKey()
    if err != nil {
        log.Fatal(err)
    }

    srv, err := dgproto.NewServer(dgproto.ServerConfig{
        StaticKey:   staticKey,
        CipherSuite: dgproto.CipherChaCha20Poly1305,
        Handler: func(_ context.Context, conn *dgproto.Connection, msg any) error {
            if data, ok := msg.(*dgproto.EncryptedData); ok {
                return conn.Send(dgproto.EncryptedData{
                    StreamID:       data.StreamID,
                    AppMessageType: data.AppMessageType,
                    Fields:         data.Fields,
                })
            }
            return nil
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    ln, err := net.Listen("tcp", "127.0.0.1:4242")
    if err != nil {
        log.Fatal(err)
    }
    defer ln.Close()

    log.Println("listening on 127.0.0.1:4242")
    if err := srv.Serve(ln); err != nil && !errors.Is(err, dgproto.ErrServerClosed) {
        log.Fatal(err)
    }
}
```

## 3. Connect a Client

The package exposes the initiator handshake as an explicit three-flight state
machine. A client must:

1. Dial TCP and wrap the connection with `NewTCPTransport`.
2. Create `NewInitiatorHandshake(clientKey, serverPublicKey)`.
3. Exchange flights 1–3 as handshake frames (`0x01`, then `0x02`).
4. Obtain `Handshake.Result()` and call `NewSessionFromHandshake`.
5. Construct `NewConnection(transport, session, config)` and call `Start(ctx)`.

This explicit API keeps identity verification and connection lifecycle under
the caller's control. See `server_test.go` for an end-to-end initiator example.
After `Start`, construct an application TLV and send the envelope:

```go
text, err := dgproto.NewTLV(1, []byte("hello"))
if err != nil {
    log.Fatal(err)
}
if err := client.Send(dgproto.EncryptedData{
    StreamID:       1,
    AppMessageType: 1,
    Fields:         []dgproto.TLV{text},
}); err != nil {
    log.Fatal(err)
}
```

`EncryptedData` contains `StreamID`, `AppMessageType`, and uniquely typed
`Fields`; it has no raw `Payload` field. Define TLV type meanings in the
application protocol shared by both peers.

## 4. Connection Configuration

`ConnectionConfig` controls timeouts, keepalives, and queue depths:

```go
dgproto.ConnectionConfig{
    HandshakeTimeout:  5 * time.Second,
    ReadTimeout:       30 * time.Second,
    WriteTimeout:      10 * time.Second,
    IdleTimeout:       2 * time.Minute,
    KeepaliveInterval: 30 * time.Second,
    KeepaliveTimeout:  10 * time.Second,
    OutboundQueue:     64,
    HandlerQueue:      64,
}
```

| Field | Default | Description |
|---|---|---|
| `HandshakeTimeout` | none | Max time for Noise XX to complete |
| `ReadTimeout` | none | Per-frame read deadline |
| `WriteTimeout` | none | Per-frame write deadline |
| `IdleTimeout` | none | Close if no inbound frames received |
| `KeepaliveInterval` | none | Send Ping after this idle period |
| `KeepaliveTimeout` | 2× interval | Close if Pong not received in time |
| `OutboundQueue` | 16 | Buffered outbound message slots |
| `HandlerQueue` | 16 | Buffered inbound dispatch slots |

## 5. Sending with Backpressure

| Method | Blocks? | Use when |
|---|---|---|
| `Send(msg)` | no — returns `ErrOutboundQueueFull` | fire-and-forget, high throughput |
| `TrySend(msg)` | no | explicit nonblocking form |
| `SendContext(ctx, msg)` | yes — until slot free or ctx cancelled | moderate backpressure tolerance |
| `SendAndWait(ctx, msg)` | yes — until transport write completes | guaranteed ordering / flow control |
| `SendPadded(msg, pad)` | no | control per-frame anti-fingerprint padding |

## 6. Error Handling

`Connection.Err()` returns the terminal cause after `Done()` is closed.
Multiple concurrent termination signals are ranked:

1. `ErrHandlerPanic` (highest)
2. Handler-returned error
3. Transport / protocol error
4. Context cancellation / `ErrConnectionClosed`
5. Clean EOF (lowest)

```go
<-client.Done()
if err := client.Err(); err != nil && !errors.Is(err, dgproto.ErrConnectionClosed) {
    log.Printf("connection terminated: %v", err)
}
```

## 7. Integration with dgpserver

For a higher-level routing, authentication, and middleware layer see
[dgpserver](https://github.com/datagram-messenger/datagram-server/tree/main/pkg/dgpserver).
It wraps this package and provides request routing, JWT-based client
authentication, and structured logging.

## Further Reading

- [Wire Specification](../protocol/dgproto-v1.md) — normative byte-level protocol definition
- [Architecture Overview](../architecture/overview.md) — package layout and data flow
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — how to contribute and run tests
