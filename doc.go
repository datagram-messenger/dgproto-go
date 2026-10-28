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
// OnDisconnect. HandlerShutdownTimeout bounds the time Close waits for a
// canceled MessageHandler to return; a positive bound detaches, not kills,
// a stuck handler. Zero preserves unbounded waiting; NewServer rejects negative
// values. Zero-valued server settings select a 10-second handshake
// timeout, queue capacities of 16, 64 concurrent handshakes, and 1024
// established connections.
//
// Server.Stats returns a secret-free advisory resource snapshot for a server
// constructed by NewServer. Pending/active ownership and shutdown are sampled
// together under one mutex, preserving socket-to-runtime handoff; active includes
// handoff before Start, not peer readiness. Slot gauges are independently sampled
// and need not agree with the maps or each other during transitions. Handshake
// slots are released before Admission; connection slots can remain occupied
// through OnDisconnect after active ownership ends. Stats reserves no capacity.
//
// DialContext requires a provisioned server static-key pin and returns a stopped
// Connection plus non-secret AdmissionInfo. Explicit Start(runtimeCtx) controls
// its lifetime independently of establishment context cancellation. Zero client
// HandshakeTimeout bounds dial plus handshake to 10 seconds (or an earlier caller
// deadline). Success is a local final-flight write, not server admission.
//
// Send and SendPaddedContext report bounded-queue acceptance. SendAndWait and
// SendPaddedAndWait wait for the local writer result; none guarantees peer receipt
// or application acknowledgement. Queued buffers must remain immutable; canceled
// write-waits do not retract accepted messages or permit immediate buffer reuse.
// Padding is random cleartext trailing bytes authenticated with the exact header;
// zero requested length means no padding. Policy remains application-owned.
//
// EncryptedData.EncodedLen reports the aligned envelope payload size without
// allocating on valid success. EncryptedData.ValidateFrameSize optionally checks
// the complete header/payload/tag budget with the exact requested uint8 padding
// before enqueueing. Neither method snapshots inputs, reserves queue/session
// state, nor guarantees a successful send or delivery; keep inputs unchanged
// through serialization or queued use. Send APIs do not preflight automatically.
//
// The first observed terminal cause is retained. Close attempts a close-frame
// write, not a peer-acknowledged handshake or queue drain. Server Abort closes
// networking without waiting for callbacks; it cannot kill handler/hook goroutines.
package dgproto
