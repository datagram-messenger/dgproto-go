package dgproto

import (
	"context"
	"errors"
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

// isTemporaryAcceptError retains the root classifier entry point for Serve
// and its regression tests; acceptpolicy.Retryable documents the policy.
func isTemporaryAcceptError(err error) bool {
	return acceptpolicy.Retryable(err)
}
