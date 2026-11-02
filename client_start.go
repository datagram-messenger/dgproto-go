package dgproto

import (
	"context"
	"fmt"
)

// DialAndStartContext establishes a pinned TCP connection with DialContext and
// starts its runtime using runtimeCtx. Configure Handler and runtime limits in
// config before calling; a handler may run before this function returns and
// should use its Connection argument, not a variable awaiting this return.
//
// establishmentCtx bounds only dial and handshake. runtimeCtx independently
// controls the started connection's lifetime; canceling establishmentCtx after
// success does not stop it. Runtime cancellation does not interrupt an ongoing
// handshake; establishmentCtx and HandshakeTimeout retain that budget. Both
// contexts must be nonnil. A runtime cancellation
// observed before dialing returns immediately; one observed after establishment
// closes the still-stopped connection and returns no connection or metadata.
// Errors preserve errors.Is/errors.As as in DialContext.
//
// On success the caller owns the connection and must Close it. Cancellation or
// another terminal event racing with Start or the return may leave the returned
// connection already stopped: observe Done and Err for runtime outcomes. Success
// is not server admission, readiness, delivery, or acknowledgement. This helper
// adds no goroutines beyond Connection.Start and does not wait for handlers or
// peer confirmation. Use DialContext instead to prepare a stopped connection.
func DialAndStartContext(establishmentCtx, runtimeCtx context.Context, address string, config ClientConfig) (*Connection, AdmissionInfo, error) {
	if establishmentCtx == nil || runtimeCtx == nil {
		return nil, AdmissionInfo{}, fmt.Errorf("%w: nil context", ErrInvalidClientConfig)
	}
	if err := runtimeCtx.Err(); err != nil {
		return nil, AdmissionInfo{}, err
	}
	conn, info, err := DialContext(establishmentCtx, address, config)
	if err != nil {
		return nil, AdmissionInfo{}, err
	}
	if err = runtimeCtx.Err(); err != nil {
		// No runtime or handler has started, so Close is synchronous and does
		// not wait for network writes or callbacks.
		_ = conn.Close()
		return nil, AdmissionInfo{}, err
	}
	conn.Start(runtimeCtx)
	return conn, info, nil
}
