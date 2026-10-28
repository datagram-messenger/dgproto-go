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

## Run a complete loopback echo

From the repository root:

```sh
go run ./examples/echo
```

Success prints `authenticated application echo received`. The [complete command](../../examples/echo/main.go) opens only `127.0.0.1:0`, allowlists its generated client key and pins the locally generated server key. It installs the receive handler before explicit `Start`, preflights a padded immutable TLV envelope, and checks the returned stream, operation, field type and payload. The application echo, not `DialContext` or local write completion, is the receipt check.

Establishment and runtime use separate bounded contexts. Cleanup always closes the caller-owned client (even if unstarted), waits for `Done`, closes the server to join its bounded work, closes the owned listener, and joins `Serve`; cleanup join contexts are independent of runtime cancellation. These illustrative loopback budgets are not deployment defaults. See [the examples index](../../examples/README.md) for limits, cancellation behavior and tests.

Ephemeral demo identities are not production provisioning or persistence; the command is not an application server, automatic reconnect, or trust-on-first-use. The existing [godoc exchange](../../client_example_test.go) remains runnable with `go test -run "^ExampleDialContext$" -count=1 -timeout=15s -v`.

## Sending and ownership

`Send`, `TrySend`, and `SendPadded` report nonblocking queue acceptance (or `ErrOutboundQueueFull`). `SendContext`/`SendPaddedContext` wait for queue capacity; `SendAndWait`/`SendPaddedAndWait` also wait for the complete local write, including required rekey. None guarantees peer receipt, processing, acknowledgement, or durable delivery. Padding is random **cleartext** trailing bytes authenticated along with the exact header; zero length means no padding. It is not traffic obfuscation.

Messages and reachable buffers are not snapshotted at enqueue. Keep them immutable while pending. A successful write-wait permits reuse; cancellation **after enqueue does not retract** the message and can return before serialization finishes, so cancellation alone does not permit buffer mutation. Use fresh immutable messages for acceptance-only sends. Application correlation, authorization, retries and idempotency remain application-owned; wire Ack is not durable delivery.

### Optional EncryptedData size preflight

Before enqueueing an application envelope, `EncryptedData.EncodedLen()` returns its actual marshaled payload length (4-byte envelope plus aligned TLVs), without allocating on valid success. It excludes the frame header, AEAD tag and random frame padding. `EncryptedData.ValidateFrameSize(padLength)` checks the **complete** wire budget with the exact `uint8` padding you intend to send:

```go
message := dgproto.EncryptedData{
    StreamID: 1,
    AppMessageType: 2,
    Fields: []dgproto.TLV{{Type: 1, Value: []byte("hello")}},
}
const padLength uint8 = 255
if err := message.ValidateFrameSize(padLength); err != nil {
    return err
}
return conn.SendPaddedAndWait(ctx, message, padLength)
```

The maximum total frame is 65535 bytes: 40-byte header + encoded payload + 16-byte tag + requested padding. Payload length is `4 + sum(align4(3 + len(value)))`; for one field, values up to 65469 bytes fit with padding 0, or 65217 with padding 255. Alignment means not every payload can use every last budget byte.

Both methods preserve marshaling validation priority: duplicate types first, then TLV count/value/sequence limits, then the envelope limit (`ErrMessageLength`). A message that marshals but is too large with the requested padding wraps `ErrPayloadTooLarge`, matching the codec's oversize diagnostic. `EncodedLen` returns zero on error. Successful calls allocate no storage and do not mutate inputs.

Preflight is optional, not automatically performed by send APIs. It is **not a snapshot, queue/session reservation, successful-send promise, or delivery guarantee**. Inputs must remain unchanged during preflight and through serialization/queued use under the ownership contract above; cancellation after enqueue still does not permit immediate reuse. Preflight lets applications reject known size errors before queueing without changing send behavior. Run the [executable example](../../integrationtests/api/encrypted_data_size_example_test.go) with `go test -run ExampleEncryptedData_ValidateFrameSize -v ./integrationtests/api`.

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

### Advisory server resource budget snapshot

`server.Stats()` returns a secret-free `ServerStats` value for budget monitoring:
handshake and connection slot usage/capacity, pending and active ownership counts,
and whether shutdown has begun. Defaults are 64 handshake slots and 1024 connection
slots; configure and measure limits for your service. See the
[executable example](../../server_stats_example_test.go) (`go test -run ExampleServer_Stats -v`).

Channel gauges are independently sampled, not a capacity reservation or an exact
cross-field transaction. The two map counts and shutdown flag are coherent under
the ownership mutex: pending includes sockets blocked in `Admission` after Noise
has released its handshake slot; active means handed-off, server-owned runtimes,
not live/ready peers. Connection slots are acquired before admission and may remain
occupied through `OnDisconnect` after active ownership ends. `Close` waits for
server-owned work; after successful `Close` and exit of `Serve`, quiescent usage
and ownership counts are zero (capacities remain). Stats exposes no secrets,
identities, payloads or errors
and is safe in callbacks; it does not change admission or shutdown policy. Use it
on servers constructed by `NewServer`, not as a nil/zero-receiver contract.

Set `ReadTimeout` to zero to rely solely on `IdleTimeout` and keepalive
liveness as the authoritative deadlines.

Production services should persist and protect the static key, set explicit
timeouts, and use `ServerConfig.Admission` for application-level authorization
after the Noise handshake.
