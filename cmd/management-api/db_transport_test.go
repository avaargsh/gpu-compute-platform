package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagementDatabaseTransportRequiresVerifiedTLS(t *testing.T) {
	dir := t.TempDir()
	rootCA := filepath.Join(dir, "root-ca.pem")
	if err := os.WriteFile(rootCA, []byte("test CA; pgx performs certificate parsing"), 0600); err != nil {
		t.Fatal(err)
	}
	uri := "postgres://api-login:never-log-this-password@db.internal:5432/gpu?sslmode=verify-full&sslrootcert=" + rootCA
	for _, tc := range []struct {
		name  string
		dsn   string
		allow bool
	}{
		{"verified CA and hostname", uri, true},
		{"postgresql scheme", strings.Replace(uri, "postgres://", "postgresql://", 1), true},
		{"verified loopback still requires CA", strings.Replace(uri, "db.internal", "127.0.0.1", 1), true},
		{"plaintext downgrade", strings.Replace(uri, "verify-full", "disable", 1), false},
		{"opportunistic downgrade", strings.Replace(uri, "verify-full", "prefer", 1), false},
		{"TLS without hostname verification", strings.Replace(uri, "verify-full", "require", 1), false},
		{"CA-only verification", strings.Replace(uri, "verify-full", "verify-ca", 1), false},
		{"absent sslmode", strings.Replace(uri, "sslmode=verify-full&", "", 1), false},
		{"absent trust root", strings.Split(uri, "&sslrootcert=")[0], false},
		{"duplicate sslmode secure-first", uri + "&sslmode=disable", false},
		{"duplicate sslmode insecure-first", strings.Replace(uri, "sslmode=verify-full", "sslmode=disable&sslmode=verify-full", 1), false},
		{"duplicate CA", uri + "&sslrootcert=/tmp/other-ca.pem", false},
		{"host override", uri + "&host=attacker.invalid", false},
		{"hostaddr override", uri + "&hostaddr=192.0.2.1", false},
		{"service override", uri + "&service=other", false},
		{"servicefile override", uri + "&servicefile=/tmp/pgservice", false},
		{"broken query encoding", uri + "&bad=%ZZ", false},
		{"unix socket URI", strings.Replace(uri, "db.internal:5432", "", 1), false},
		{"multi-host URI", strings.Replace(uri, "db.internal", "db.internal,other.internal", 1), false},
		{"missing database", strings.Replace(uri, "/gpu?", "/?", 1), false},
		{"missing user", strings.Replace(uri, "api-login:never-log-this-password@", "", 1), false},
		{"relative CA path", strings.Replace(uri, rootCA, "ca.pem", 1), false},
		{"missing CA file", strings.Replace(uri, rootCA, filepath.Join(dir, "missing.pem"), 1), false},
		{"CA is directory", strings.Replace(uri, rootCA, dir, 1), false},
		{"fragment smuggling", uri + "#other", false},
		{"invalid URL", "invalid connection string", false},
		{"empty URL", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateManagementDatabaseTransport(tc.dsn)
			if tc.allow && err != nil {
				t.Fatalf("verified transport rejected: %v", err)
			}
			if !tc.allow && err == nil {
				t.Fatal("invalid or unsafe DB transport admitted")
			}
			if err != nil && strings.Contains(err.Error(), "never-log-this-password") {
				t.Fatal("DB transport rejection leaked credentials")
			}
		})
	}
}
