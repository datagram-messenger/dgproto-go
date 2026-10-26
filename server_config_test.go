package dgproto

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestHardeningNegativeHandlerShutdownTimeout(t *testing.T) {
	config := validServerConfig(t)
	config.HandlerShutdownTimeout = -time.Second
	if _, err := NewServer(config); err == nil {
		t.Fatal("negative HandlerShutdownTimeout accepted with valid static key")
	}
}

func TestNewServerRejectsInvalidStaticKeyEarly(t *testing.T) {
	if _, err := NewServer(ServerConfig{}); !errors.Is(err, ErrInvalidStaticKey) {
		t.Fatalf("zero key error = %v", err)
	}
	key := generateTestStaticKey(t)
	key.public[0] ^= 1
	if _, err := NewServer(ServerConfig{StaticKey: key}); !errors.Is(err, ErrInvalidStaticKey) {
		t.Fatalf("mismatched key error = %v", err)
	}
}

func TestNewServerTimeoutConsistency(t *testing.T) {
	config := validServerConfig(t)
	config.ReadTimeout = time.Second
	config.IdleTimeout = 2 * time.Second
	if _, err := NewServer(config); err == nil {
		t.Fatal("expected read timeout shorter than idle timeout to fail")
	}
	config.ReadTimeout = 0
	if _, err := NewServer(config); err != nil {
		t.Fatalf("disabled read timeout: %v", err)
	}
}

func TestNewServerNormalizesKeepaliveTimeoutBeforeValidation(t *testing.T) {
	tests := []struct {
		name             string
		readTimeout      time.Duration
		keepaliveTimeout time.Duration
		wantErr          bool
		wantTimeout      time.Duration
	}{
		{name: "default rejected by short read timeout", readTimeout: 2 * time.Second, wantErr: true},
		{name: "default accepted and stored", readTimeout: 3 * time.Second, wantTimeout: 2 * time.Second},
		{name: "explicit timeout accepted", readTimeout: 1500 * time.Millisecond, keepaliveTimeout: 500 * time.Millisecond, wantTimeout: 500 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validServerConfig(t)
			config.ReadTimeout = test.readTimeout
			config.KeepaliveInterval = time.Second
			config.KeepaliveTimeout = test.keepaliveTimeout

			server, err := NewServer(config)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected liveness validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if server.config.KeepaliveTimeout != test.wantTimeout {
				t.Fatalf("KeepaliveTimeout = %v, want %v", server.config.KeepaliveTimeout, test.wantTimeout)
			}
		})
	}
}

func TestNewServerPreservesKeepaliveOverflowValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  func(ServerConfig) ServerConfig
		wantErr string
	}{
		{
			name: "unrepresentable default",
			config: func(config ServerConfig) ServerConfig {
				config.KeepaliveInterval = time.Duration(math.MaxInt64/2 + 1)
				return config
			},
			wantErr: "dgproto: KeepaliveInterval is too large",
		},
		{
			name: "unrepresentable liveness window",
			config: func(config ServerConfig) ServerConfig {
				config.KeepaliveInterval = time.Duration(math.MaxInt64)
				config.KeepaliveTimeout = 1
				return config
			},
			wantErr: "dgproto: keepalive liveness window is too large",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewServer(test.config(validServerConfig(t)))
			if err == nil || err.Error() != test.wantErr {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestNewServerAdmissionDefaults(t *testing.T) {
	server, err := NewServer(validServerConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if server.config.HandlerQueue != 16 {
		t.Fatalf("HandlerQueue = %d", server.config.HandlerQueue)
	}
	if server.config.MaxConcurrentHandshakes != 64 {
		t.Fatalf("MaxConcurrentHandshakes = %d", server.config.MaxConcurrentHandshakes)
	}
	if server.config.MaxActiveConnections != 1024 {
		t.Fatalf("MaxActiveConnections = %d", server.config.MaxActiveConnections)
	}
}

func TestNewServerRejectsNegativeAdmissionLimits(t *testing.T) {
	tests := []ServerConfig{
		{MaxConcurrentHandshakes: -1},
		{MaxActiveConnections: -1},
	}
	for _, config := range tests {
		if _, err := NewServer(config); err == nil {
			t.Fatalf("expected error for config %#v", config)
		}
	}
}

func TestNewServerRejectsNonMVPCiphers(t *testing.T) {
	for _, suite := range []CipherSuite{CipherAES256GCM, 99} {
		if _, err := NewServer(ServerConfig{CipherSuite: suite}); !errors.Is(err, ErrUnsupportedCipher) {
			t.Fatalf("suite %d error = %v", suite, err)
		}
	}
}

func TestNewServerConfigurationSnapshot(t *testing.T) {
	config := validServerConfig(t)
	config.KeepaliveInterval = time.Second
	original := config
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, original) {
		t.Fatal("NewServer changed the caller's configuration")
	}
	want := original
	want.CipherSuite = CipherChaCha20Poly1305
	want.HandshakeTimeout = 10 * time.Second
	want.KeepaliveTimeout = 2 * time.Second
	want.OutboundQueue = 16
	want.HandlerQueue = 16
	want.MaxConcurrentHandshakes = 64
	want.MaxActiveConnections = 1024
	if !reflect.DeepEqual(server.config, want) {
		t.Fatal("stored configuration does not match the complete normalized snapshot")
	}
	if cap(server.handshakes) != want.MaxConcurrentHandshakes || cap(server.connSlots) != want.MaxActiveConnections {
		t.Fatal("slot capacities do not match normalized configuration")
	}
	if server.listener != nil || len(server.conns) != 0 || len(server.handshakeConns) != 0 || server.inShutdown.Load() {
		t.Fatal("constructor started or populated runtime state")
	}
	select {
	case <-server.closed:
		t.Fatal("new server is already closed")
	default:
	}
}

func TestNewServerPreservesExplicitConfiguration(t *testing.T) {
	config := validServerConfig(t)
	config.CipherSuite = CipherChaCha20Poly1305
	config.AllowedClients = [][]byte{config.StaticKey.Public()}
	config.HandshakeTimeout = time.Second
	config.ReadTimeout = 4 * time.Second
	config.WriteTimeout = time.Second
	config.IdleTimeout = 2 * time.Second
	config.KeepaliveInterval = time.Second
	config.KeepaliveTimeout = time.Second
	config.HandlerShutdownTimeout = time.Second
	config.OutboundQueue = 3
	config.HandlerQueue = 5
	config.MaxConcurrentHandshakes = 7
	config.MaxActiveConnections = 11
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(server.config, config) {
		t.Fatal("constructor changed explicit configuration")
	}
	if cap(server.handshakes) != 7 || cap(server.connSlots) != 11 {
		t.Fatal("constructor changed explicit slot capacities")
	}
}

func TestNewServerConfigurationErrorPriority(t *testing.T) {
	config := validServerConfig(t)
	config.MaxConcurrentHandshakes = -1
	config.MaxActiveConnections = -1
	server, err := NewServer(config)
	if server != nil || err == nil || err.Error() != "dgproto: MaxConcurrentHandshakes must not be negative" {
		t.Fatalf("NewServer() = %v, %v; want first invalid admission limit", server, err)
	}
	config = ServerConfig{CipherSuite: CipherAES256GCM, HandshakeTimeout: -1}
	server, err = NewServer(config)
	if server != nil || !errors.Is(err, ErrUnsupportedCipher) {
		t.Fatalf("NewServer() = %v, %v; want cipher failure before key/timeout", server, err)
	}
}
