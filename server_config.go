package dgproto

import (
	"errors"
	"fmt"
	"math"
	"net"
	"time"
)

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
