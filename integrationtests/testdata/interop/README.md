# Opt-in Go/Rust Error Session loopback

From the Go repository, build this driver to a temporary location:

```sh
go build -o /tmp/dgproto-error-peer ./integrationtests/testdata/interop
```

Run that executable; its first stdout line is a loopback address. From the Rust
repository, set `DGPROTO_ERROR_INTEROP_ADDR` to that address and run:

```sh
cargo test --locked --offline --lib go_rust_error_session_interop -- --ignored --nocapture
```

Use an appropriate temporary executable path on Windows. The listener expires
after 20 seconds, Go I/O after 15 seconds; Rust has a 15-second outer timeout
and five-second frame I/O deadlines. Kill the peer process on test failure and
remove only the temporary executable you created.

Both peers use the real library Session and TCPTransport implementations with
public-safe synthetic directional keys, not user key files. Error codes 3/`hi`
and 255/`Ошибка` travel in both directions, with meaning assertions. Go sends
the literal raw `03006869` through SendPayload in one direction; both codecs
also produce nonempty text. Authenticated frame padding, Ping/Pong and unchanged
TLV Close are checked. The Rust harness is test-only and introduces no public
API or exhaustive ApplicationMessage variant.

Scope: real cross-language encrypted Session/frame/message interoperability.
It deliberately initializes synthetic session secrets rather than performing
Noise establishment, and does not exercise Connection queues or deploy policy.
Do not describe this as a full handshake/Connection interoperability test.
