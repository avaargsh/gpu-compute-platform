package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHumanListenerIsExplicitAndFailClosed(t *testing.T) {
	complete := map[string]string{
		"CONTROL_PLANE_OAUTH_INTROSPECTION_URL": "https://idp.example/introspect",
		"CONTROL_PLANE_OAUTH_ISSUER": "https://idp.example/",
		"CONTROL_PLANE_OAUTH_AUDIENCE": "gpu-api",
		"CONTROL_PLANE_OAUTH_CLIENT_ID": "gpu-cp",
		"CONTROL_PLANE_OAUTH_CLIENT_SECRET_FILE": "/var/run/secrets/oauth/client-secret",
		"CONTROL_PLANE_HUMAN_TLS_CERT": "/etc/https/server.crt",
		"CONTROL_PLANE_HUMAN_TLS_KEY": "/etc/https/server.key",
	}
	if cfg, err := LoadHumanListener(getEnv(nil), "poc"); err != nil || cfg.Enabled { t.Fatalf("disabled= %#v %v", cfg, err) }
	if cfg, err := LoadHumanListener(getEnv(complete), "poc"); err != nil || !cfg.Enabled { t.Fatalf("enabled= %#v %v", cfg, err) }
	for key := range complete {
		vars := map[string]string{}
		for k, v := range complete { if key != k { vars[k] = v } }
		if _, err := LoadHumanListener(getEnv(vars), "poc"); err == nil { t.Errorf("accepted missing %s", key) }
	}
	for _, profile := range []string{"development", "production"} {
		if _, err := LoadHumanListener(getEnv(complete), profile); err == nil { t.Errorf("%s incorrectly enabled", profile) }
	}
}

func TestHumanListenerReadsCredentialOnlyFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-secret")
	if err := os.WriteFile(path, []byte("secret-value\n"), 0600); err != nil { t.Fatal(err) }
	secret, err := (HumanListener{ClientSecretFile: path}).ReadClientSecret()
	if err != nil || secret != "secret-value" { t.Fatalf("secret file unreadable: %v", err) }
	_, err = (HumanListener{ClientSecretFile: path + "-missing"}).ReadClientSecret()
	if err == nil { t.Fatal("missing credential file accepted") }
}
