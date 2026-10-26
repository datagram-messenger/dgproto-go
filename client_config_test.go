package dgproto

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestClientConfigNormalizationDefaultsAndOwnership(t *testing.T) {
	config, _ := clientTestConfig(t)
	pin := append([]byte(nil), config.ServerStatic...)
	config.KeepaliveInterval = time.Second
	config.IdleTimeout = 3 * time.Second
	config.ReadTimeout = 3 * time.Second // exact liveness boundary
	config.OutboundQueue, config.HandlerQueue = 7, 9
	original := config
	normalized, err := normalizeClientConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	want := original.ConnectionConfig
	want.HandshakeTimeout = 10 * time.Second
	want.KeepaliveTimeout = 2 * time.Second
	if !reflect.DeepEqual(normalized.ConnectionConfig, want) || normalized.StaticKey != original.StaticKey {
		t.Fatalf("normalized config = %+v, want %+v", normalized.ConnectionConfig, want)
	}
	if !reflect.DeepEqual(config, original) {
		t.Fatal("normalization changed caller config")
	}
	config.ServerStatic[0] ^= 1
	if !bytes.Equal(normalized.ServerStatic, pin) {
		t.Fatal("normalized pin aliases caller")
	}
	normalized.ServerStatic[1] ^= 1
	if config.ServerStatic[1] != pin[1] {
		t.Fatal("caller pin aliases normalized result")
	}
}

func TestClientConfigNormalizationExplicitAndDisabledLiveness(t *testing.T) {
	config, _ := clientTestConfig(t)
	config.ConnectionConfig = ConnectionConfig{
		HandshakeTimeout: time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: time.Second, IdleTimeout: 4 * time.Second,
		KeepaliveInterval: 2 * time.Second, KeepaliveTimeout: 3 * time.Second,
		HandlerShutdownTimeout: time.Second, OutboundQueue: 3, HandlerQueue: 4,
	}
	got, err := normalizeClientConfig(config)
	if err != nil || !reflect.DeepEqual(got.ConnectionConfig, config.ConnectionConfig) {
		t.Fatalf("explicit settings: %+v, %v", got.ConnectionConfig, err)
	}
	config.ReadTimeout = 0
	config.KeepaliveInterval = 0
	config.KeepaliveTimeout = time.Duration(math.MaxInt64)
	if _, err := normalizeClientConfig(config); err != nil {
		t.Fatalf("zero read deadline and representable disabled-interval window: %v", err)
	}
}

func TestClientConfigNormalizationFailurePriorityAndSnapshot(t *testing.T) {
	good, _ := clientTestConfig(t)
	for _, tc := range []struct {
		name   string
		change func(*ClientConfig)
		want   error
	}{
		{"identity before pin and timeouts", func(c *ClientConfig) { c.StaticKey = StaticKey{}; c.ServerStatic = nil; c.ReadTimeout = -1 }, ErrInvalidStaticKey},
		{"pin before timeouts", func(c *ClientConfig) { c.ServerStatic = nil; c.ReadTimeout = -1 }, ErrInvalidClientConfig},
		{"negative handler queue", func(c *ClientConfig) { c.HandlerQueue = -1 }, ErrInvalidClientConfig},
		{"negative read timeout", func(c *ClientConfig) { c.ReadTimeout = -1 }, ErrInvalidClientConfig},
		{"negative write timeout", func(c *ClientConfig) { c.WriteTimeout = -1 }, ErrInvalidClientConfig},
		{"negative idle timeout", func(c *ClientConfig) { c.IdleTimeout = -1 }, ErrInvalidClientConfig},
		{"negative keepalive interval", func(c *ClientConfig) { c.KeepaliveInterval = -1 }, ErrInvalidClientConfig},
		{"negative keepalive timeout", func(c *ClientConfig) { c.KeepaliveTimeout = -1 }, ErrInvalidClientConfig},
		{"doubling overflow", func(c *ClientConfig) { c.KeepaliveInterval = time.Duration(math.MaxInt64/2 + 1) }, ErrInvalidClientConfig},
		{"window overflow", func(c *ClientConfig) { c.KeepaliveInterval = 1; c.KeepaliveTimeout = time.Duration(math.MaxInt64) }, ErrInvalidClientConfig},
		{"read below window", func(c *ClientConfig) { c.KeepaliveInterval = time.Second; c.ReadTimeout = 3*time.Second - 1 }, ErrInvalidClientConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := good
			tc.change(&config)
			original := config
			_, err := normalizeClientConfig(config)
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(config, original) {
				t.Fatalf("error=%v caller changed=%v", err, !reflect.DeepEqual(config, original))
			}
		})
	}
}

func TestClientConfigCurrentErrorChain(t *testing.T) {
	old := ErrInvalidClientConfig
	t.Cleanup(func() { ErrInvalidClientConfig = old })
	config, _ := clientTestConfig(t)
	config.ServerStatic = nil
	for _, replacement := range []error{errors.New("replacement config sentinel"), nil} {
		ErrInvalidClientConfig = replacement
		_, err := normalizeClientConfig(config)
		if err == nil || errors.Unwrap(err) != replacement {
			t.Fatalf("current sentinel wrapper = %v", err)
		}
	}
}
