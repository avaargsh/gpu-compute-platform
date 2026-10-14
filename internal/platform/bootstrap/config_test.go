package bootstrap

import (
	"strings"
	"testing"
)

const goodDSN = "postgres://platform_runtime@platform-db-rw.security.svc:5432/platform?sslmode=verify-full&sslrootcert=/etc/pg/ca.crt&sslcert=/etc/pg/tls.crt&sslkey=/etc/pg/tls.key"

func getEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestProfileDefaultsToLoopbackDevelopment(t *testing.T) {
	cfg, err := Load(getEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "development" || cfg.ListenAddr != "127.0.0.1:8080" || cfg.DatabaseURL != "" {
		t.Fatalf("unexpected default config: %#v", cfg)
	}
}

func TestLocalDevelopmentFixtureRemainsCompatible(t *testing.T) {
	cfg, err := Load(getEnv(map[string]string{
		"CONTROL_PLANE_PROFILE": "development",
		"DATABASE_URL": "postgres://gpu:gpu@127.0.0.1:5432/gpu_platform?sslmode=disable",
		"CONTROL_PLANE_LISTEN_ADDR": "127.0.0.1:9090",
	}))
	if err != nil || cfg.ListenAddr != "127.0.0.1:9090" {
		t.Fatalf("local fixture should remain possible: %#v %v", cfg, err)
	}
}

func TestAllProfilesRejectNonLoopbackListener(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "10.0.0.4:8080", "localhost:garbage", "localhost:0"} {
		t.Run(addr, func(t *testing.T) {
			if _, err := Load(getEnv(map[string]string{"CONTROL_PLANE_LISTEN_ADDR": addr})); err == nil {
				t.Fatalf("listener %q was accepted", addr)
			}
		})
	}
}

func TestPocRequiresExplicitCertificateVerifiedDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn string
	}{
		{"missing", ""},
		{"plaintext", "postgres://platform_runtime@db:5432/platform?sslmode=disable"},
		{"weak-tls", "postgres://platform_runtime@db:5432/platform?sslmode=require"},
		{"password", "postgres://platform_runtime:pass@db:5432/platform?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key"},
		{"no-cert", "postgres://platform_runtime@db:5432/platform?sslmode=verify-full&sslrootcert=/ca"},
		{"relative-private-key", "postgres://platform_runtime@db:5432/platform?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=secret.key"},
		{"duplicate-sslmode", goodDSN + "&sslmode=disable"},
		{"override-host", goodDSN + "&host=evil.example"},
		{"override-service", goodDSN + "&service=default"},
		{"unknown-parameter", goodDSN + "&passfile=/tmp/pgpass"},
		{"missing-db", "postgres://platform_runtime@db:5432/?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key"},
		{"nested-db", "postgres://platform_runtime@db:5432/platform/other?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key"},
		{"no-user", "postgres://db:5432/platform?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key"},
		{"wrong-scheme", "mysql://platform_runtime@db:5432/platform?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key"},
		{"unsupported-host", "postgres://platform_runtime@db:5432/platform?sslmode=verify-full&sslrootcert=/ca&sslcert=/cert&sslkey=/key&hostaddr=1.2.3.4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(getEnv(map[string]string{
				"CONTROL_PLANE_PROFILE": "poc", "DATABASE_URL": tc.dsn,
			}))
			if err == nil {
				t.Fatalf("%s passed validation", tc.name)
			}
			if strings.Contains(err.Error(), "pass@") || strings.Contains(err.Error(), "passfile") {
				t.Fatalf("error must not echo credentials or DSN: %v", err)
			}
		})
	}
}

func TestPocAcceptsPinnedTLSTrustAndLoopback(t *testing.T) {
	cfg, err := Load(getEnv(map[string]string{
		"CONTROL_PLANE_PROFILE": "poc",
		"DATABASE_URL": goodDSN,
		"CONTROL_PLANE_LISTEN_ADDR": "[::1]:8080",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "poc" || cfg.DatabaseURL != goodDSN {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestPocForbidsAmbientPGOverrides(t *testing.T) {
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPASSWORD", "PGSERVICE", "PGOPTIONS", "PGSSLMODE"} {
		t.Run(key, func(t *testing.T) {
			_, err := Load(getEnv(map[string]string{
				"CONTROL_PLANE_PROFILE": "poc", "DATABASE_URL": goodDSN, key: "nonempty",
			}))
			if err == nil {
				t.Fatalf("ambient %s was accepted", key)
			}
		})
	}
}

func TestProductionRemainsBlockedWithoutOIDCAndAgentIdentity(t *testing.T) {
	for _, vars := range []map[string]string{
		{"CONTROL_PLANE_PROFILE": "production", "DATABASE_URL": goodDSN},
		{"ENVIRONMENT": "production", "DATABASE_URL": goodDSN},
		{"ENVIRONMENT": "staging", "DATABASE_URL": goodDSN},
	} {
		if _, err := Load(getEnv(vars)); err == nil || !strings.Contains(err.Error(), "authentication") && !strings.Contains(err.Error(), "authenticated") {
			t.Fatalf("must block production profile: %v", err)
		}
	}
}

func TestLegacyProductionCannotBeDowngradedExplicitly(t *testing.T) {
	_, err := Load(getEnv(map[string]string{
		"ENVIRONMENT": "production", "CONTROL_PLANE_PROFILE": "development",
	}))
	if err == nil {
		t.Fatal("legacy production must not be downgraded")
	}
}

func TestUnknownProfileFailsClosed(t *testing.T) {
	_, err := Load(getEnv(map[string]string{"CONTROL_PLANE_PROFILE": "staging"}))
	if err == nil {
		t.Fatal("unsupported profile accepted")
	}
}
