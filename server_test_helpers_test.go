package dgproto

import "testing"

func generateTestStaticKey(t *testing.T) StaticKey {
	t.Helper()
	key, err := GenerateStaticKey()
	if err != nil {
		t.Fatalf("failed to generate static key: %v", err)
	}
	return key
}

func validServerConfig(t *testing.T) ServerConfig {
	t.Helper()
	return ServerConfig{StaticKey: generateTestStaticKey(t)}
}
