package bootstrap

import (
	"os"
	"strings"
	"testing"
)

func TestAgentMTLSListenerConfigurationFailClosed(t *testing.T) {
	full := map[string]string{
		"CONTROL_PLANE_TLS_CERT":           "/etc/tls/server.crt",
		"CONTROL_PLANE_TLS_KEY":            "/etc/tls/server.key",
		"CONTROL_PLANE_AGENT_CLIENT_CA":    "/etc/tls/client-ca.crt",
		"CONTROL_PLANE_AGENT_TRUST_DOMAIN": "platform.example",
	}
	cfg, err := LoadAgentListener(getEnv(nil), "poc")
	if err != nil || cfg.Enabled {
		t.Fatalf("disabled should be accepted: %#v %v", cfg, err)
	}
	cfg, err = LoadAgentListener(getEnv(full), "poc")
	if err != nil || !cfg.Enabled || cfg.TrustDomain != "platform.example" {
		t.Fatalf("complete poc should be accepted: %#v %v", cfg, err)
	}
	for _, profile := range []string{"development", "production"} {
		_, err = LoadAgentListener(getEnv(full), profile)
		if err == nil {
			t.Fatalf("accepted agent mTLS with profile %q", profile)
		}
	}
	for key := range full {
		partial := make(map[string]string, len(full))
		for k, v := range full {
			partial[k] = v
		}
		delete(partial, key)
		_, err = LoadAgentListener(getEnv(partial), "poc")
		if err == nil {
			t.Fatalf("accepted missing %s", key)
		}
	}
}

func TestAgentTLSConfigRequiresTrustedClientCA(t *testing.T) {
	_, err := AgentTLSConfig("")
	if err == nil {
		t.Fatal("empty client CA accepted")
	}
	dir := t.TempDir()
	path := dir + "/not-a-ca.pem"
	if err := os.WriteFile(path, []byte("not a PEM certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = AgentTLSConfig(path)
	if err == nil || !strings.Contains(err.Error(), "no certificates") {
		t.Fatalf("invalid CA accepted: %v", err)
	}
}
