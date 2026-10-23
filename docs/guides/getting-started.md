# Getting started

DGProto is a protocol and session library, not an application server. `DialContext` provides a TCP client with a mandatory provisioned server static-key pin. Lower-level handshake/session primitives remain available.

## Pinned client

`ClientConfig` embeds `ConnectionConfig`: set `ConnectionConfig.HandshakeTimeout` in a composite literal. Zero selects 10 seconds for the **entire dial plus handshake**, bounded earlier by the caller context. Negative durations/queue capacities, missing client identity, missing/non-32-byte/all-zero server pins, and inconsistent read/liveness settings are rejected before dialing. There is no unverified default, retry, reconnect, or trust-on-first-use.

```go
// clientKey and serverPublic are securely provisioned application inputs.
conn, info, err := dgproto.DialContext(establishmentCtx, address, dgproto.ClientConfig{
    StaticKey: clientKey,
    ServerStatic: serverPublic,
    ConnectionConfig: dgproto.ConnectionConfig{
        HandshakeTimeout: 5 * time.Second,
        WriteTimeout: 5 * time.Second,
        Handler: handler,
    },
})
if err != nil {
    return err
}
defer conn.Close()
_ = info // PeerStatic, SessionID, RemoteAddr; no traffic secrets.
conn.Start(runtimeCtx) // explicit: returned connection is STOPPED.
```

Canceling establishment after success does not cancel the established lifetime. The caller owns the connection, including closing an unstarted connection. Success confirms the pinned key and a complete **local** final-flight write, not server admission, readiness, or application authorization. Pin provisioning, persistence and rotation are application responsibilities. Metadata is non-secret but may still be privacy-sensitive.

For a complete runnable, bounded, client/server exchange, run `go test -run ExampleDialContext -v`. See [the executable example](../../client_example_test.go). Its ephemeral demo identities are not a production key-management strategy; the example is not a messaging application server.

## Sending and ownership

`Send`, `TrySend`, and `SendPadded` report nonblocking queue acceptance (or `ErrOutboundQueueFull`). `SendContext`/`SendPaddedContext` wait for queue capacity; `SendAndWait`/`SendPaddedAndWait` also wait for the complete local write, including required rekey. None guarantees peer receipt, processing, acknowledgement, or durable delivery. Padding is random **cleartext** trailing bytes authenticated along with the exact header; zero length means no padding. It is not traffic obfuscation.

Messages and reachable buffers are not snapshotted at enqueue. Keep them immutable while pending. A successful write-wait permits reuse; cancellation **after enqueue does not retract** the message and can return before serialization finishes, so cancellation alone does not permit buffer mutation. Use fresh immutable messages for acceptance-only sends. Application correlation, authorization, retries and idempotency remain application-owned; wire Ack is not durable delivery.

## Server

This complete example generates a server static key, creates a TCP listener, and passes the listener to `Server.Serve`:

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
    key, err := dgproto.GenerateStaticKey()
    if err != nil {
        log.Fatal(err)
    }

    listener, err := net.Listen("tcp", ":9000")
    if err != nil {
        log.Fatal(err)
    }

    server, err := dgproto.NewServer(dgproto.ServerConfig{
        StaticKey: key,
        Handler: func(ctx context.Context, conn *dgproto.Connection, message any) error {
            log.Printf("received %T", message)
            return nil
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := server.Serve(listener); err != nil && !errors.Is(err, dgproto.ErrServerClosed) {
        log.Fatal(err)
    }
}
```

Production services should persist and protect the static key instead of generating a new identity on every start.

## Admission and authorization

Noise XX authenticates the peer static key; application authorization remains a separate step. `ServerConfig.AllowedClients` can restrict connections to an allowlist of client static public keys. `ServerConfig.Admission` runs after successful Noise authentication and before message dispatch, so applications can apply additional authorization policy without receiving traffic secrets.

## Lifecycle

* `Connection.Close()` attempts a close-frame write then closes transport; it is not a peer acknowledgement or queue drain. `Done()` tracks runtime completion and `Err()` preserves the first terminal cause.
* `Server.Close()` stops networking and waits for server-owned work. A positive `HandlerShutdownTimeout` bounds the post-cancellation handler wait by detaching it, not killing it; zero can wait indefinitely.
* `Server.Abort()` closes networking without waiting for handlers/hooks. Synchronous admission and disconnect hooks must bound their own I/O; abort does not kill goroutines or guarantee a later `Close` returns.

All three methods return an error; callers should handle it.

## Production timeout guidance

`NewServer` uses a 10-second `HandshakeTimeout` when the configured value is zero;
zero does not disable the handshake deadline. `ReadTimeout`, `WriteTimeout`,
`IdleTimeout`, and `KeepaliveInterval` remain zero (disabled) by default. When
keepalive is enabled with a zero `KeepaliveTimeout`, its effective timeout is
twice `KeepaliveInterval`.
For internet-facing deployments, set explicit values to bound resource usage:

| Field | Recommended starting point | Purpose |
|---|---|---|
| `HandshakeTimeout` | 10 s (server default) | Bound unauthenticated TCP connections |
| `ReadTimeout` | ≥ `KeepaliveInterval` + `KeepaliveTimeout` | Per-frame network read deadline; must not be shorter than the keepalive liveness window |
| `WriteTimeout` | 5–30 s | Per-frame network write deadline |
| `IdleTimeout` | 5–15 min | Close connections with no inbound activity |
| `KeepaliveInterval` | 30–60 s | How often an encrypted Ping is sent |
| `KeepaliveTimeout` | 10–30 s | How long an unanswered Ping is tolerated before closing |

`NewServer` and `DialContext` reject a positive `ReadTimeout` shorter than
`max(IdleTimeout, KeepaliveInterval + KeepaliveTimeout)`. Zero is permitted.
The illustrative table is not a universal production budget; choose values from
measured service constraints and test slow/stalled peers and shutdown.
Set `ReadTimeout` to zero to rely solely on `IdleTimeout` and keepalive
liveness as the authoritative deadlines.

Production services should persist and protect the static key, set explicit
timeouts, and use `ServerConfig.Admission` for application-level authorization
after the Noise handshake.
