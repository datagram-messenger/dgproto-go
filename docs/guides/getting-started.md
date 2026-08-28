# Getting started

DGProto is a protocol and session library, not an application server. It does not expose a high-level client dial-and-handshake helper. Clients must provide transport integration and drive the exported handshake and session primitives for their deployment.

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

* `Connection.Close()` performs the connection close handshake.
* `Server.Close()` stops the listener and gracefully closes active connections.
* `Server.Abort()` immediately stops the server and active connections.

All three methods return an error; callers should handle it.
