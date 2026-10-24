package dgproto

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/datagram-messenger/dgproto-go/internal/acceptpolicy"
)

var (
	// ErrServerClosed is returned when operating on a server that has been shut down.
	ErrServerClosed = errors.New("dgproto: server closed")

	// ErrHandshakeFailed is returned when a Noise XX handshake cannot be completed.
	ErrHandshakeFailed = errors.New("dgproto: handshake failed")

	// ErrClientNotPermitted is returned when an authenticated client static key is not in AllowedClients.
	ErrClientNotPermitted = errors.New("dgproto: client static key not permitted")
)

// AdmissionInfo is the non-secret identity and transport metadata available
// after a successful Noise handshake and before application dispatch.
type AdmissionInfo struct {
	PeerStatic [32]byte
	SessionID  [16]byte
	RemoteAddr net.Addr
}

// AdmissionHandler may accept or reject an authenticated connection before it
// starts. Returning an error closes only that connection.
type AdmissionHandler func(context.Context, *Connection, AdmissionInfo) error

// DisconnectHandler observes termination of a connection that passed Admission.
// The supplied context is owned by the caller and may be detached and bounded.
type DisconnectHandler func(context.Context, *Connection, AdmissionInfo, error)

// ServerConfig configures the runtime parameters and cryptographic settings for a Server.
type ServerConfig struct {
	// StaticKey is the server's long-term Noise static key pair.
	StaticKey StaticKey

	// AllowedClients is an optional allowlist of 32-byte client static public keys.
	// If empty, any client presenting a valid Noise handshake is permitted.
	AllowedClients [][]byte

	// CipherSuite sets the symmetric encryption algorithm used after handshake.
	CipherSuite CipherSuite

	// HandshakeTimeout controls the maximum duration allowed to complete the Noise XX exchange.
	HandshakeTimeout time.Duration

	// ReadTimeout sets an optional network read deadline per frame. Zero disables
	// it so IdleTimeout and keepalive liveness remain the authoritative deadlines.
	ReadTimeout time.Duration

	// WriteTimeout sets the network write deadline per frame.
	WriteTimeout time.Duration

	// IdleTimeout controls the maximum time a connection may remain idle.
	IdleTimeout time.Duration

	// KeepaliveInterval controls how often an encrypted Ping is sent.
	KeepaliveInterval time.Duration

	// KeepaliveTimeout controls how long an outstanding Ping may remain unacknowledged.
	KeepaliveTimeout time.Duration

	// OutboundQueue sets the channel capacity for buffered outbound messages.
	OutboundQueue int

	// HandlerQueue bounds pending per-connection handler work.
	HandlerQueue int

	// MaxConcurrentHandshakes limits TCP connections concurrently performing a handshake.
	MaxConcurrentHandshakes int

	// MaxActiveConnections limits authenticated connections retained by the server.
	MaxActiveConnections int

	// Handler processes authenticated inbound messages received on active connections.
	Handler MessageHandler

	// HandlerShutdownTimeout bounds how long Close waits for an in-progress
	// MessageHandler call to return after the connection context is canceled.
	// Zero disables the bound (Close may block indefinitely on a stuck handler).
	// Positive values detach a stuck handler, not kill it. NewServer rejects
	// negative values. Abort does not wait for handlers.
	HandlerShutdownTimeout time.Duration

	// Admission runs after Noise authentication and before the connection starts.
	// It exposes identity metadata but no traffic secrets.
	Admission AdmissionHandler

	// OnDisconnect runs exactly once after an admitted connection terminates.
	OnDisconnect DisconnectHandler
}

// Server accepts incoming TCP connections, completes Noise XX handshakes,
// and manages connection lifecycles.
type Server struct {
	config         ServerConfig
	listener       net.Listener
	mu             sync.Mutex
	conns          map[*Connection]struct{}
	handshakeConns map[net.Conn]struct{}
	handshakes     chan struct{}
	connSlots      chan struct{}
	closed         chan struct{}
	inShutdown     atomic.Bool
	wg             sync.WaitGroup
}

// NewServer constructs a Server instance with applied defaults for unconfigured timeouts.
func NewServer(config ServerConfig) (*Server, error) {
	if config.CipherSuite == 0 {
		config.CipherSuite = CipherChaCha20Poly1305
	}
	if config.CipherSuite != CipherChaCha20Poly1305 {
		return nil, fmt.Errorf("%w: DGProto v1 requires ChaCha20-Poly1305", ErrUnsupportedCipher)
	}
	if err := config.StaticKey.validate(); err != nil {
		return nil, err
	}
	if config.HandshakeTimeout < 0 {
		return nil, errors.New("dgproto: HandshakeTimeout must not be negative")
	}
	if config.ReadTimeout < 0 || config.WriteTimeout < 0 || config.IdleTimeout < 0 || config.KeepaliveInterval < 0 {
		return nil, errors.New("dgproto: connection timeouts must not be negative")
	}
	if config.HandlerShutdownTimeout < 0 {
		return nil, errors.New("dgproto: HandlerShutdownTimeout must not be negative")
	}
	if config.KeepaliveTimeout < 0 {
		return nil, errors.New("dgproto: KeepaliveTimeout must not be negative")
	}
	if config.KeepaliveInterval > 0 && config.KeepaliveTimeout == 0 {
		var representable bool
		config.KeepaliveTimeout, representable = defaultKeepaliveTimeout(config.KeepaliveInterval)
		if !representable {
			return nil, errors.New("dgproto: KeepaliveInterval is too large")
		}
	}
	if config.KeepaliveInterval > 0 && config.KeepaliveTimeout > time.Duration(math.MaxInt64)-config.KeepaliveInterval {
		return nil, errors.New("dgproto: keepalive liveness window is too large")
	}
	if config.ReadTimeout > 0 {
		livenessWindow := config.IdleTimeout
		if keepaliveWindow := config.KeepaliveInterval + config.KeepaliveTimeout; keepaliveWindow > livenessWindow {
			livenessWindow = keepaliveWindow
		}
		if livenessWindow > 0 && config.ReadTimeout < livenessWindow {
			return nil, errors.New("dgproto: ReadTimeout must be zero or at least the idle/keepalive liveness window")
		}
	}
	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = 10 * time.Second
	}
	if config.OutboundQueue < 0 {
		return nil, errors.New("dgproto: OutboundQueue must not be negative")
	}
	if config.OutboundQueue == 0 {
		config.OutboundQueue = 16
	}
	if config.HandlerQueue < 0 {
		return nil, errors.New("dgproto: HandlerQueue must not be negative")
	}
	if config.HandlerQueue == 0 {
		config.HandlerQueue = 16
	}
	if config.MaxConcurrentHandshakes < 0 {
		return nil, errors.New("dgproto: MaxConcurrentHandshakes must not be negative")
	}
	if config.MaxConcurrentHandshakes == 0 {
		config.MaxConcurrentHandshakes = 64
	}
	if config.MaxActiveConnections < 0 {
		return nil, errors.New("dgproto: MaxActiveConnections must not be negative")
	}
	if config.MaxActiveConnections == 0 {
		config.MaxActiveConnections = 1024
	}
	return &Server{
		config:         config,
		conns:          make(map[*Connection]struct{}),
		handshakeConns: make(map[net.Conn]struct{}),
		handshakes:     make(chan struct{}, config.MaxConcurrentHandshakes),
		connSlots:      make(chan struct{}, config.MaxActiveConnections),
		closed:         make(chan struct{}),
	}, nil
}

// Serve binds listener and processes incoming connections until Close or listener failure.
func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("dgproto: nil listener")
	}
	s.mu.Lock()
	if s.inShutdown.Load() {
		s.mu.Unlock()
		return ErrServerClosed
	}
	s.listener = listener
	s.mu.Unlock()

	var acceptDelay time.Duration
	for {
		netConn, err := listener.Accept()
		if err != nil {
			if s.inShutdown.Load() {
				return ErrServerClosed
			}
			if isTemporaryAcceptError(err) {
				if acceptDelay == 0 {
					acceptDelay = 5 * time.Millisecond
				} else {
					acceptDelay *= 2
				}
				acceptDelay = min(acceptDelay, time.Second)
				timer := time.NewTimer(acceptDelay)
				select {
				case <-timer.C:
					continue
				case <-s.closed:
					if !timer.Stop() {
						<-timer.C
					}
					return ErrServerClosed
				}
			}
			return err
		}
		acceptDelay = 0

		if !s.tryAcquire(s.handshakes) {
			_ = netConn.Close()
			continue
		}
		if !s.registerHandshake(netConn) {
			s.release(s.handshakes)
			_ = netConn.Close()
			continue
		}

		go func() {
			defer s.wg.Done()
			s.handleConn(netConn)
		}()
	}
}

// Close stops accepting new connections, gracefully closes active connections,
// and waits for all server-owned goroutines. Concurrent calls are safe.
func (s *Server) Close() error {
	listener, conns, handshakeConns := s.beginShutdown()
	var err error
	if listener != nil {
		err = listener.Close()
	}
	for _, conn := range handshakeConns {
		_ = conn.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	s.wg.Wait()
	return err
}

// Abort force-closes listener and network transports without waiting for
// handlers or disconnect hooks. It is safe to call concurrently with Close and
// is intended for deadline escalation by a higher-level shutdown coordinator.
func (s *Server) Abort() error {
	listener, conns, handshakeConns := s.beginShutdown()
	var err error
	if listener != nil {
		err = listener.Close()
	}
	for _, conn := range handshakeConns {
		_ = conn.Close()
	}
	for _, conn := range conns {
		conn.abort(ErrConnectionClosed)
	}
	return err
}

func (s *Server) beginShutdown() (net.Listener, []*Connection, []net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inShutdown.Swap(true) {
		close(s.closed)
	}
	conns := make([]*Connection, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	handshakeConns := make([]net.Conn, 0, len(s.handshakeConns))
	for conn := range s.handshakeConns {
		handshakeConns = append(handshakeConns, conn)
	}
	return s.listener, conns, handshakeConns
}

func (s *Server) handleConn(netConn net.Conn) {
	transport := NewTCPTransport(netConn)
	// Retain socket ownership through admission and until atomic handoff.
	defer s.unregisterHandshake(netConn)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), s.config.HandshakeTimeout)
	session, result, err := s.performHandshake(ctx, transport)
	cancel()
	s.release(s.handshakes)
	if err != nil {
		_ = transport.Close()
		return
	}
	if !s.tryAcquire(s.connSlots) {
		_ = transport.Close()
		return
	}
	defer s.release(s.connSlots)

	connConfig := ConnectionConfig{
		OutboundQueue:          s.config.OutboundQueue,
		HandlerQueue:           s.config.HandlerQueue,
		HandshakeTimeout:       s.config.HandshakeTimeout,
		ReadTimeout:            s.config.ReadTimeout,
		WriteTimeout:           s.config.WriteTimeout,
		IdleTimeout:            s.config.IdleTimeout,
		KeepaliveInterval:      s.config.KeepaliveInterval,
		KeepaliveTimeout:       s.config.KeepaliveTimeout,
		Handler:                s.config.Handler,
		HandlerShutdownTimeout: s.config.HandlerShutdownTimeout,
	}

	conn := NewConnection(transport, session, connConfig)
	if s.config.Admission != nil {
		admissionCtx, admissionCancel := context.WithTimeout(context.Background(), s.config.HandshakeTimeout)
		err = callAdmission(s.config.Admission, admissionCtx, conn, AdmissionInfo{
			PeerStatic: result.PeerStatic,
			SessionID:  result.SessionID,
			RemoteAddr: netConn.RemoteAddr(),
		})
		admissionCancel()
		if err != nil {
			_ = conn.Close()
			return
		}
	}
	if s.config.OnDisconnect != nil {
		defer func() {
			callDisconnect(s.config.OnDisconnect, context.Background(), conn, AdmissionInfo{
				PeerStatic: result.PeerStatic,
				SessionID:  result.SessionID,
				RemoteAddr: netConn.RemoteAddr(),
			}, conn.Err())
		}()
	}
	if !s.registerConn(conn) {
		_ = conn.Close()
		return
	}
	defer s.unregisterConn(conn)

	connCtx, connCancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-s.closed:
			_ = conn.Close()
		case <-connCtx.Done():
		}
	}()
	defer connCancel()

	conn.Start(connCtx)
	<-conn.Done()
}

func callAdmission(handler AdmissionHandler, ctx context.Context, conn *Connection, info AdmissionInfo) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: admission panic: %v", ErrHandshakeFailed, recovered)
		}
	}()
	return handler(ctx, conn, info)
}

func callDisconnect(handler DisconnectHandler, ctx context.Context, conn *Connection, info AdmissionInfo, cause error) {
	defer func() { _ = recover() }()
	handler(ctx, conn, info, cause)
}

func (s *Server) tryAcquire(slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) release(slots chan struct{}) {
	<-slots
}

func (s *Server) registerHandshake(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inShutdown.Load() {
		return false
	}
	s.handshakeConns[conn] = struct{}{}
	// Reserve the worker before shutdown can acquire mu and begin Wait.
	s.wg.Add(1)
	return true
}

func (s *Server) unregisterHandshake(conn net.Conn) {
	s.mu.Lock()
	delete(s.handshakeConns, conn)
	s.mu.Unlock()
}

func (s *Server) registerConn(conn *Connection) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inShutdown.Load() {
		return false
	}
	s.conns[conn] = struct{}{}
	// The runtime replaces the socket entry under the same shutdown lock.
	delete(s.handshakeConns, conn.transport.conn)
	return true
}

func (s *Server) unregisterConn(conn *Connection) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

// isTemporaryAcceptError retains the root classifier entry point for Serve
// and its regression tests; acceptpolicy.Retryable documents the policy.
func isTemporaryAcceptError(err error) bool {
	return acceptpolicy.Retryable(err)
}
