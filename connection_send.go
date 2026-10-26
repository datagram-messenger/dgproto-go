package dgproto

import "context"

// Send queues a message without waiting for network I/O. It returns
// ErrOutboundQueueFull rather than blocking when the queue is full.
func (c *Connection) Send(message any) error { return c.SendPadded(message, 0) }

// TrySend is the explicit nonblocking form of Send.
func (c *Connection) TrySend(message any) error { return c.Send(message) }

// SendContext waits for outbound queue capacity or context cancellation. It
// returns after enqueueing and does not wait for the local transport write.
// Buffer ownership and cancellation follow SendPaddedContext.
func (c *Connection) SendContext(ctx context.Context, message any) error {
	return c.SendPaddedContext(ctx, message, 0)
}

// SendPaddedContext waits for queue acceptance with 0–255 bytes of random,
// authenticated cleartext frame padding. Nil ctx means Background. Cancellation
// after acceptance does not retract the message. The message and all reachable
// buffers must remain immutable while pending; acceptance is not write completion
// or peer acknowledgement. Use a fresh immutable message for admission-only sends.
func (c *Connection) SendPaddedContext(ctx context.Context, message any, padLength uint8) error {
	return c.enqueue(ctx, queuedMessage{message: message, padLength: padLength}, true)
}

// SendAndWait waits for queue capacity and for the local transport write to
// complete. Completion does not imply that the peer processed the message.
// Buffer ownership and cancellation follow SendPaddedAndWait.
func (c *Connection) SendAndWait(ctx context.Context, message any) error {
	return c.SendPaddedAndWait(ctx, message, 0)
}

// SendPaddedAndWait waits for capacity and the complete local padded write,
// including any required rekey through the existing single writer. Nil ctx means
// Background. Success permits buffer reuse, but is not peer receipt, processing,
// or acknowledgement. Cancellation after enqueue can return before serialization
// finishes and does not retract the message: do not mutate its reachable buffers
// merely because a canceled call returned. Zero padding requests no padding.
func (c *Connection) SendPaddedAndWait(ctx context.Context, message any, padLength uint8) error {
	completion := make(chan error, 1)
	if err := c.enqueue(ctx, queuedMessage{message: message, padLength: padLength, completion: completion}, true); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-completion:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		select {
		case err := <-completion:
			return err
		default:
			return context.Cause(c.ctx)
		}
	}
}

func (c *Connection) enqueue(ctx context.Context, item queuedMessage, wait bool) error {
	return c.enqueueChannel(ctx, c.outbound, item, wait)
}

func (c *Connection) enqueueControl(message any, wait bool) error {
	return c.enqueueChannel(c.ctx, c.control, queuedMessage{message: message}, wait)
}

func (c *Connection) enqueueChannel(ctx context.Context, queue chan<- queuedMessage, item queuedMessage, wait bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c.queueMu.RLock()
	defer c.queueMu.RUnlock()
	// A ready queue must not win over cancellation that predates this call.
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closing.Load() {
		return ErrConnectionClosed
	}
	if !wait {
		select {
		case queue <- item:
			return nil
		default:
			return ErrOutboundQueueFull
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return ErrConnectionClosed
	case queue <- item:
		return nil
	}
}

// SendPadded queues a message with the requested encrypted-frame padding. It
// does not wait for network I/O and returns ErrOutboundQueueFull on backpressure.
func (c *Connection) SendPadded(message any, padLength uint8) error {
	return c.enqueue(context.TODO(), queuedMessage{message: message, padLength: padLength}, false)
}

func (c *Connection) writeLoop() {
	defer c.failPending()
	for {
		select {
		case request := <-c.closeRequest:
			err := c.writeMessage(queuedMessage{message: request.message})
			request.result <- err
			c.shutdown(request.cause)
			return
		default:
		}

		select {
		case <-c.ctx.Done():
			return
		case request := <-c.closeRequest:
			err := c.writeMessage(queuedMessage{message: request.message})
			request.result <- err
			c.shutdown(request.cause)
			return
		case item := <-c.control:
			err := c.writeMessage(item)
			complete(item, err)
			if err != nil {
				c.shutdown(err)
				return
			}
		case item := <-c.outbound:
			err := c.writeMessage(item)
			complete(item, err)
			if err != nil {
				c.shutdown(err)
				return
			}
		}
	}
}

func complete(item queuedMessage, err error) {
	if item.completion != nil {
		item.completion <- err
	}
}

func (c *Connection) failPending() {
	err := context.Cause(c.ctx)
	if err == nil {
		err = ErrConnectionClosed
	}
	for {
		select {
		case item := <-c.control:
			complete(item, err)
		case item := <-c.outbound:
			complete(item, err)
		case request := <-c.closeRequest:
			request.result <- err
		default:
			return
		}
	}
}

// writeMessage retries the original message after any automatically generated
// RekeyInit frame so rekeying never drops the caller's message.
func (c *Connection) writeMessage(item queuedMessage) error {
	for {
		frame, err := c.session.Send(item.message, item.padLength)
		if err != nil {
			return err
		}
		ctx, cancel := c.operationContext(c.config.WriteTimeout)
		err = c.transport.WriteFrame(ctx, frame)
		cancel()
		if err != nil {
			return err
		}
		if frame.Header.MessageType != MessageTypeRekeyInit {
			return nil
		}
		if err := c.session.MarkRekeySent(frame); err != nil {
			return err
		}
	}
}
