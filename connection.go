package dgproto

import (
	"context"
	"errors"
	"fmt"
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
