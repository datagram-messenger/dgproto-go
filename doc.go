// Package dgproto implements the draft DGProto v1 wire protocol and secure
// session runtime over TCP. It is a protocol library, not an application
// server.
//
// The implementation spans L0 TCP framing, L1 strict frame parsing, L2 Noise
// XX and ChaCha20-Poly1305, L3 epochs, rekey, replay and lifecycle, and L4
// typed messages and dispatch. docs/protocol/dgproto-v1.md is normative for
// wire-visible behavior.
//
// ServerConfig supports authenticated-key filtering with AllowedClients,
// application authorization with Admission, and termination observation with
// OnDisconnect. Zero-valued server settings select a 10-second handshake
// timeout, queue capacities of 16, 64 concurrent handshakes, and 1024
// established connections.
//
// Send reports bounded-queue acceptance. SendAndWait waits for the local writer
// result; neither operation guarantees peer receipt or application
// acknowledgement. The first observed terminal cause is retained. Abort is
// immediate, server shutdown uses Close or immediate Abort, and padding policy belongs to
// the application.
package dgproto
