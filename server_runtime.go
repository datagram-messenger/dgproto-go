package dgproto

import (
	"context"
	"fmt"
	"net"
)

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
