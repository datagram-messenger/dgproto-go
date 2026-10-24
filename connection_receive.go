package dgproto

import (
	"fmt"
	"time"
)

func (c *Connection) readLoop() {
	for {
		ctx, cancel := c.operationContext(c.config.ReadTimeout)
		frame, err := c.transport.ReadFrame(ctx)
		cancel()
		if err != nil {
			c.shutdown(err)
			return
		}
		message, err := c.session.Receive(frame)
		if err != nil {
			c.shutdown(err)
			return
		}
		c.noteActivity()
		dispatch := true
		switch m := message.(type) {
		case *PingPong:
			dispatch = false
			if m.IsResponse {
				c.acknowledgePing(m.Nonce)
			} else if err := c.enqueueControl(PingPong{IsResponse: true, Nonce: m.Nonce}, true); err != nil {
				c.shutdown(err)
				return
			}
		case *RekeyInit:
			dispatch = false
		case *SessionClose:
			c.shutdown(ErrConnectionClosed)
			return
		}
		if dispatch && c.config.Handler != nil {
			c.queueMu.RLock()
			if c.ctx.Err() != nil {
				c.queueMu.RUnlock()
				return
			}
			select {
			case c.handlerQueue <- message:
				c.queueMu.RUnlock()
			default:
				c.queueMu.RUnlock()
				c.shutdown(ErrHandlerQueueFull)
				return
			}
		}
	}
}

func (c *Connection) handlerLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		select {
		case <-c.ctx.Done():
			return
		case message := <-c.handlerQueue:
			// Cancellation may race with the receive. Recheck before starting
			// user code; queued callbacks are never begun after shutdown.
			select {
			case <-c.ctx.Done():
				return
			default:
			}
			if err := c.callHandlerBounded(message); err != nil {
				c.shutdown(err)
				return
			}
		}
	}
}

// callHandlerBounded calls the handler and, if HandlerShutdownTimeout is set
// and the connection context is canceled while the handler is running, waits
// at most that long for the handler to return. If the deadline expires the
// handler goroutine is detached — it may still run to completion, but
// handlerLoop unblocks so the connection waitgroup can finish.
func (c *Connection) callHandlerBounded(message any) error {
	timeout := c.config.HandlerShutdownTimeout
	if timeout <= 0 {
		// No bound configured: run inline, preserving the original behavior.
		return c.callHandler(message)
	}

	// Run the handler in a goroutine so we can race it against the shutdown
	// deadline without blocking handlerLoop indefinitely.
	result := make(chan error, 1)
	go func() { result <- c.callHandler(message) }()

	select {
	case err := <-result:
		// Handler finished before any deadline — fast path, no timer needed.
		return err
	case <-c.ctx.Done():
		// Connection is shutting down. Give the handler a bounded window.
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		// Handler did not return within the deadline. Detach it; the goroutine
		// will eventually finish on its own (result channel is buffered).
		return nil
	}
}

func (c *Connection) callHandler(message any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, recovered)
		}
	}()
	return c.config.Handler(c.ctx, c, message)
}
