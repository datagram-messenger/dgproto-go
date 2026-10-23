# Changelog

## 0.4.2

### Fixed

- `Connection.SendContext` and `Connection.SendAndWait` now reject contexts
  canceled before the call without admitting a message to an available queue.
- `Session.Send` returns `ErrMessageType` instead of panicking for nil pointers
  to supported session messages, without consuming a send sequence number.
- `Connection.Close` completes shutdown and closes `Done` when called before
  `Start`. A subsequent `Start` does not launch runtime loops. Concurrent
  `Start` and `Close` coordinate startup and shutdown ownership.

### Documentation

- README and getting-started guidance now correctly describe the existing
  10-second server handshake timeout: a zero `HandshakeTimeout` selects that
  default rather than disabling the deadline. Connection timeout guidance also
  clarifies the conditional keepalive timeout default.

### Compatibility

No wire format, cryptographic derivation, message type, protocol limit, exported
Go signature, dependency, or minimum Go version changes from 0.4.1. The documented
timeout defaults are unchanged. No migration is needed for valid messages and
non-canceled send contexts. DGProto v1 remains a draft; this patch does not establish
cross-implementation compatibility or claim vulnerability fixes.
