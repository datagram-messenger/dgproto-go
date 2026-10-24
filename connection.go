package dgproto

import (
	"context"
	"errors"
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

func (c *Connection) operationContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(c.ctx)
	}
	return context.WithTimeout(c.ctx, timeout)
}
