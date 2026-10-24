package dgproto

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrConnectionClosed indicates that the connection runtime has terminated.
	ErrConnectionClosed = errors.New("dgproto: connection closed")
	// ErrIdleTimeout indicates that inbound activity did not occur before the idle timeout.
	ErrIdleTimeout = errors.New("dgproto: connection idle timeout")
	// ErrKeepaliveTimeout indicates that an outstanding ping was not acknowledged in time.
	ErrKeepaliveTimeout = errors.New("dgproto: keepalive timeout")
	// ErrOutboundQueueFull indicates that a nonblocking send found the outbound queue full.
	ErrOutboundQueueFull = errors.New("dgproto: outbound queue full")
	// ErrHandlerQueueFull indicates that pending handler work exceeded its bound.
	ErrHandlerQueueFull = errors.New("dgproto: handler queue full")
	// ErrHandlerPanic wraps a panic recovered from a MessageHandler.
	ErrHandlerPanic = errors.New("dgproto: handler panic")
)

// MessageHandler handles one authenticated inbound message. The runtime calls
// handlers serially on a dedicated worker; returning an error or panicking
// closes the connection. The context is canceled when the connection terminates.
type MessageHandler func(context.Context, *Connection, any) error

// ConnectionConfig controls an established connection runtime.
type ConnectionConfig struct {
	OutboundQueue int
	HandlerQueue  int
	// HandshakeTimeout bounds DialContext establishment (zero selects 10 seconds).
	// NewConnection accepts an already-established session and does not use it.
	HandshakeTimeout  time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	KeepaliveInterval time.Duration
	KeepaliveTimeout  time.Duration
	Handler           MessageHandler
	// HandlerShutdownTimeout bounds how long the connection waits for an
	// in-progress handler call to return after the connection context is
	// canceled. Zero disables the bound.
	HandlerShutdownTimeout time.Duration
}

func defaultKeepaliveTimeout(interval time.Duration) (time.Duration, bool) {
	if interval > time.Duration(math.MaxInt64/2) {
		return time.Duration(math.MaxInt64), false
	}
	return 2 * interval, true
}

type queuedMessage struct {
	message    any
	padLength  uint8
	completion chan error
}

type closeRequest struct {
	message SessionClose
	cause   error
	result  chan error
}

// Connection runs an already-established Session over a TCPTransport. Its
// exported methods are safe for concurrent use.
type Connection struct {
	transport *TCPTransport
	session   *Session
	config    ConnectionConfig

	ctx          context.Context
	cancel       context.CancelCauseFunc
	outbound     chan queuedMessage
	control      chan queuedMessage
	closeRequest chan closeRequest
	activity     chan struct{}
	pong         chan uint64
	handlerQueue chan any
	done         chan struct{}

	startOnce    sync.Once
	shutdownOnce sync.Once
	closeOnce    sync.Once
	// Producers hold a read lease through enqueue. Shutdown cancels capacity
	// waiters before taking the exclusive lease to dispose terminal queues.
	queueMu   sync.RWMutex
	causeMu   sync.Mutex
	terminal  error
	started   atomic.Bool
	closing   atomic.Bool
	pingNonce atomic.Uint64
	// pong is a coalesced wakeup only. The protected acknowledgement cannot
	// be displaced by stale responses, nor lost when a timer is also ready.
	pingMu           sync.Mutex
	pingExpected     uint64
	pingAcknowledged bool
	wg               sync.WaitGroup
}

// NewConnection constructs a stopped established-session runtime.
func NewConnection(transport *TCPTransport, session *Session, config ConnectionConfig) *Connection {
	if transport == nil || session == nil {
		panic("dgproto: nil connection dependency")
	}
	if config.OutboundQueue <= 0 {
		config.OutboundQueue = 16
	}
	if config.HandlerQueue <= 0 {
		config.HandlerQueue = 16
	}
	if config.KeepaliveInterval > 0 && config.KeepaliveTimeout <= 0 {
		config.KeepaliveTimeout, _ = defaultKeepaliveTimeout(config.KeepaliveInterval)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Connection{
		transport:    transport,
		session:      session,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		outbound:     make(chan queuedMessage, config.OutboundQueue),
		control:      make(chan queuedMessage, 16),
		closeRequest: make(chan closeRequest, 1),
		activity:     make(chan struct{}, 1),
		pong:         make(chan uint64, 1),
		handlerQueue: make(chan any, config.HandlerQueue),
		done:         make(chan struct{}),
	}
}

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

// beginPing publishes the expectation before enqueue, including for a reply
// arriving before enqueueControl returns. Zero is reserved across rollover.
func (c *Connection) beginPing() uint64 {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	if c.ctx.Err() != nil {
		return 0
	}
	nonce := c.pingNonce.Load() + 1
	if nonce == 0 {
		nonce = 1
	}
	c.pingExpected, c.pingAcknowledged = nonce, false
	c.pingNonce.Store(nonce)
	return nonce
}

func (c *Connection) clearPing() {
	c.pingMu.Lock()
	c.pingExpected, c.pingAcknowledged = 0, false
	c.pingMu.Unlock()
}

func (c *Connection) acknowledgePing(nonce uint64) {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	if c.pingExpected == 0 || nonce != c.pingExpected || c.ctx.Err() != nil {
		return
	}
	c.pingExpected, c.pingAcknowledged = 0, true
	select {
	case c.pong <- nonce:
	default:
	}
}

func (c *Connection) consumePingAcknowledgement() bool {
	c.pingMu.Lock()
	defer c.pingMu.Unlock()
	acknowledged := c.pingAcknowledged
	c.pingAcknowledged = false
	return acknowledged
}

func (c *Connection) maintenanceLoop() {
	var idleTimer, keepaliveTimer, pongTimer *time.Timer
	var idle, keepalive, pongDeadline <-chan time.Time
	if c.config.IdleTimeout > 0 {
		idleTimer = time.NewTimer(c.config.IdleTimeout)
		idle = idleTimer.C
		defer idleTimer.Stop()
	}
	if c.config.KeepaliveInterval > 0 {
		keepaliveTimer = time.NewTimer(c.config.KeepaliveInterval)
		keepalive = keepaliveTimer.C
		defer keepaliveTimer.Stop()
	}
	var outstanding uint64

	for {
		select {
		case <-c.ctx.Done():
			if pongTimer != nil {
				pongTimer.Stop()
			}
			return
		case <-c.activity:
			resetTimer(idleTimer, c.config.IdleTimeout)
		case <-c.pong:
			if outstanding != 0 && c.consumePingAcknowledgement() {
				outstanding = 0
				if pongTimer != nil {
					if !pongTimer.Stop() {
						select {
						case <-pongTimer.C:
						default:
						}
					}
					pongDeadline = nil
				}
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				keepalive = keepaliveTimer.C
			}
		case <-keepalive:
			if outstanding != 0 {
				continue
			}
			outstanding = c.beginPing()
			if outstanding == 0 {
				return
			}
			if err := c.enqueueControl(PingPong{Nonce: outstanding}, false); err != nil {
				c.clearPing()
				outstanding = 0
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				continue
			}
			pongTimer = time.NewTimer(c.config.KeepaliveTimeout)
			pongDeadline = pongTimer.C
			keepalive = nil
		case <-pongDeadline:
			// A ready timer must not beat an acknowledgement already recorded
			// by the reader just because select chose the timer wakeup first.
			if c.consumePingAcknowledgement() {
				outstanding = 0
				pongDeadline = nil
				resetTimer(keepaliveTimer, c.config.KeepaliveInterval)
				keepalive = keepaliveTimer.C
				continue
			}
			c.shutdown(ErrKeepaliveTimeout)
			return
		case <-idle:
			_ = c.closeGracefully(SessionClose{Code: 3, Reason: "idle timeout"}, ErrIdleTimeout)
			return
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

func (c *Connection) noteActivity() {
	select {
	case c.activity <- struct{}{}:
	default:
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func (c *Connection) operationContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(c.ctx)
	}
	return context.WithTimeout(c.ctx, timeout)
}

func (c *Connection) callHandler(message any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, recovered)
		}
	}()
	return c.config.Handler(c.ctx, c, message)
}
