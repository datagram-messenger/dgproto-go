package dgproto

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidClientConfig indicates a missing server pin or invalid client settings.
var ErrInvalidClientConfig = errors.New("dgproto: invalid client config")

// ClientConfig configures a pinned-identity TCP client. StaticKey and
// ServerStatic are required. The embedded ConnectionConfig controls the stopped
// runtime and establishment timeout; no second timeout setting takes precedence.
// Do not mutate ServerStatic concurrently with DialContext.
type ClientConfig struct {
	StaticKey StaticKey
	// ServerStatic is the intended server's provisioned 32-byte X25519 public key.
	// There is no trust-on-first-use or unverified default.
	ServerStatic []byte
	ConnectionConfig
}

func normalizeClientConfig(config ClientConfig) (ClientConfig, error) {
	if err := config.StaticKey.validate(); err != nil {
		return config, err
	}
	if len(config.ServerStatic) != noiseKeySize || allZero(config.ServerStatic) {
		return config, fmt.Errorf("%w: a nonzero 32-byte server pin is required", ErrInvalidClientConfig)
	}
	c := &config.ConnectionConfig
	if c.HandshakeTimeout < 0 || c.ReadTimeout < 0 || c.WriteTimeout < 0 || c.IdleTimeout < 0 || c.KeepaliveInterval < 0 || c.KeepaliveTimeout < 0 || c.HandlerShutdownTimeout < 0 || c.OutboundQueue < 0 || c.HandlerQueue < 0 {
		return config, fmt.Errorf("%w: timeouts and queue capacities must not be negative", ErrInvalidClientConfig)
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.KeepaliveInterval > 0 && c.KeepaliveTimeout == 0 {
		var ok bool
		c.KeepaliveTimeout, ok = defaultKeepaliveTimeout(c.KeepaliveInterval)
		if !ok {
			return config, fmt.Errorf("%w: keepalive interval overflow", ErrInvalidClientConfig)
		}
	}
	if c.KeepaliveTimeout > time.Duration(math.MaxInt64)-c.KeepaliveInterval {
		return config, fmt.Errorf("%w: keepalive window overflow", ErrInvalidClientConfig)
	}
	window := max(c.IdleTimeout, c.KeepaliveInterval+c.KeepaliveTimeout)
	if c.ReadTimeout > 0 && c.ReadTimeout < window {
		return config, fmt.Errorf("%w: ReadTimeout must cover the idle/keepalive window or be zero", ErrInvalidClientConfig)
	}
	config.ServerStatic = append([]byte(nil), config.ServerStatic...)
	return config, nil
}
