package dgproto

import (
	"context"
	"time"
)

// Start begins the connection runtime until ctx or the connection ends.
// It has no effect after Close. A pre-canceled context stops the connection
// synchronously without starting runtime loops or dispatching queued work.
func (c *Connection) Start(ctx context.Context) {
	c.startOnce.Do(func() {
		if err := ctx.Err(); err != nil {
			// This startOnce owner has no loops to join and owns Done. Do not
			// enter closeGracefully here: it also acquires startOnce.
			c.shutdown(err)
			close(c.done)
			return
		}
		c.started.Store(true)
		c.wg.Go(c.readLoop)
		c.wg.Go(c.writeLoop)
		c.wg.Go(c.maintenanceLoop)
		if c.config.Handler != nil {
			c.wg.Go(c.handlerLoop)
		}
		go func() {
			select {
			case <-ctx.Done():
				// Record cancellation before the peer can observe SessionClose and
				// close the transport, racing readLoop to publish EOF first.
				c.recordTerminal(ctx.Err())
				_ = c.closeGracefully(SessionClose{Code: 0, Reason: "context canceled"}, ctx.Err())
			case <-c.ctx.Done():
			}
		}()
		go func() {
			c.wg.Wait()
			c.shutdown(context.Cause(c.ctx))
			close(c.done)
		}()
	})
}

// Close sends a normal SessionClose when the runtime is active, then stops it.
func (c *Connection) Close() error {
	return c.closeGracefully(SessionClose{Code: 0, Reason: "local shutdown"}, ErrConnectionClosed)
}

// Done closes after all runtime loops exit and terminal queues are disposed.
// A detached handler may still retain its in-progress message.
func (c *Connection) Done() <-chan struct{} { return c.done }

// Err returns the first terminal cause, or nil while running. Once recorded,
// the cause remains stable for all waiters and lifecycle hooks.
func (c *Connection) Err() error {
	c.causeMu.Lock()
	defer c.causeMu.Unlock()
	return c.terminal
}

func (c *Connection) closeGracefully(message SessionClose, cause error) error {
	var result error
	c.closeOnce.Do(func() {
		// Local shutdown is already a terminal event. Publish it before the
		// peer can react to SessionClose with EOF, without replacing an earlier
		// runtime failure or canceling the context needed for the close write.
		c.recordTerminal(cause)
		c.closing.Store(true)
		// Claim the unstarted runtime, or wait for concurrent Start to finish
		// installing its loops. Only the owner of startOnce closes done.
		c.startOnce.Do(func() {
			c.shutdown(cause)
			close(c.done)
		})
		if !c.started.Load() {
			return
		}
		request := closeRequest{message: message, cause: cause, result: make(chan error, 1)}
		gracePeriod := c.config.WriteTimeout
		if gracePeriod <= 0 {
			gracePeriod = 100 * time.Millisecond
		}
		timer := time.NewTimer(gracePeriod)
		defer timer.Stop()
		c.queueMu.RLock()
		if c.ctx.Err() != nil {
			c.queueMu.RUnlock()
			result = context.Cause(c.ctx)
			return
		}
		select {
		case c.closeRequest <- request:
			c.queueMu.RUnlock()
			select {
			case result = <-request.result:
			case <-c.ctx.Done():
				// writeLoop publishes the close write result before it cancels
				// the connection. Prefer that result when both are ready.
				select {
				case result = <-request.result:
				default:
					result = context.Cause(c.ctx)
				}
			case <-timer.C:
				result = context.DeadlineExceeded
			}
		case <-c.ctx.Done():
			c.queueMu.RUnlock()
			result = context.Cause(c.ctx)
		case <-timer.C:
			c.queueMu.RUnlock()
			result = context.DeadlineExceeded
		}
		c.shutdown(cause)
	})
	return result
}

func (c *Connection) shutdown(cause error) {
	if cause == nil {
		cause = ErrConnectionClosed
	}
	c.recordTerminal(cause)
	c.shutdownOnce.Do(func() {
		c.closing.Store(true)
		c.cancel(cause)
		c.clearPing()
		// Cancellation releases blocked producers before the exclusive lease.
		// No producer can append after this terminal disposal.
		c.queueMu.Lock()
		c.failPending()
		for len(c.handlerQueue) > 0 {
			select {
			case <-c.handlerQueue:
			default:
			}
		}
		c.queueMu.Unlock()
		_ = c.session.Close()
		_ = c.transport.Close()
	})
}

func (c *Connection) recordTerminal(cause error) {
	c.causeMu.Lock()
	if c.terminal == nil {
		c.terminal = cause
	}
	c.causeMu.Unlock()
}

// abort force-closes network I/O without waiting for the graceful close path.
func (c *Connection) abort(cause error) { c.shutdown(cause) }
