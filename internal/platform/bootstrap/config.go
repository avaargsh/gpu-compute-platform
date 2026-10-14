// Package bootstrap validates control-plane startup configuration before any
// network listener or database connection is created.
//
// This is an intentionally narrow security guard. It is NOT a substitute for
// the user and Cluster Agent authentication/authorization still required for
// production exposure.
package bootstrap

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Profile    string
	ListenAddr string
	DatabaseURL string
}

// Load accepts an injected environment lookup to make fail-closed behavior
// independently testable without mutating process-global variables.
func Load(getenv func(string) string) (Config, error) {
	profile := strings.TrimSpace(getenv("CONTROL_PLANE_PROFILE"))
	if profile == "" {
		profile = "development"
	}
	legacy := strings.ToLower(strings.TrimSpace(getenv("ENVIRONMENT")))
	// The older environment variable must never silently downgrade a production
	// deployment into an anonymous development API.
	if legacy == "production" || legacy == "staging" {
		if profile != "production" && strings.TrimSpace(getenv("CONTROL_PLANE_PROFILE")) != "" {
			return Config{}, errors.New("ENVIRONMENT and CONTROL_PLANE_PROFILE have incompatible security modes")
		}
		profile = "production"
	}

	cfg := Config{
		Profile:     profile,
		ListenAddr:  strings.TrimSpace(getenv("CONTROL_PLANE_LISTEN_ADDR")),
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8080"
	}
	if err := validateLoopback(cfg.ListenAddr); err != nil {
		return Config{}, err
	}

	switch cfg.Profile {
	case "development":
		// Preserve local in-memory and password-based development fixtures.
		return cfg, nil
	case "poc":
		if err := validateCertificateURL(cfg.DatabaseURL); err != nil {
			return Config{}, err
		}
		// No ambient libpq/pgx fallback configuration may redirect the DSN to
		// another database or change its SSL trust settings.
		for _, name := range []string{
			"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER",
			"PGPASSWORD", "PGSERVICE", "PGSERVICEFILE", "PGSSLMODE",
			"PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGOPTIONS",
		} {
			if getenv(name) != "" {
				return Config{}, fmt.Errorf("poc profile forbids ambient %s override", name)
			}
		}
		return cfg, nil
	case "production":
		// The Go Router has no trusted human/Agent identity enforcement yet.
		// Never expose it by accepting a production DSN alone.
		return Config{}, errors.New("production profile is blocked until authenticated human and Cluster Agent API boundaries are implemented and accepted")
	default:
		return Config{}, errors.New("CONTROL_PLANE_PROFILE must be development, poc, or production")
	}
}

func validateLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("CONTROL_PLANE_LISTEN_ADDR must be host:port on loopback")
	}
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return errors.New("unauthenticated Go API must listen on loopback only")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("CONTROL_PLANE_LISTEN_ADDR must contain a valid TCP port")
	}
	return nil
}

// validateCertificateURL protects the CNPG mTLS PoC from implicit sslmode
// fallbacks, passwords and libpq URL query parameter host redirection.
// It validates configuration shape, not live certificate trust (verified
// later by the PostgreSQL driver during TLS connection establishment).
func validateCertificateURL(dsn string) error {
	if dsn == "" {
		return errors.New("poc profile requires DATABASE_URL; in-memory storage is prohibited")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		u.Opaque != "" || u.Fragment != "" || u.Hostname() == "" {
		return errors.New("poc DATABASE_URL must be a PostgreSQL URI with an explicit host")
	}
	if u.User == nil || u.User.Username() == "" {
		return errors.New("poc DATABASE_URL must specify a certificate-bound database role")
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		return errors.New("poc DATABASE_URL must not contain a database password")
	}
	if u.Path == "" || u.Path == "/" || strings.Count(strings.TrimPrefix(u.Path, "/"), "/") > 0 {
		return errors.New("poc DATABASE_URL must specify exactly one database")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("poc DATABASE_URL contains invalid port")
		}
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("poc DATABASE_URL query must be valid")
	}
	allowed := map[string]bool{
		"sslmode": true, "sslrootcert": true, "sslcert": true, "sslkey": true,
		"connect_timeout": true, "application_name": true,
	}
	for k, values := range params {
		if !allowed[k] || len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			return errors.New("poc DATABASE_URL has an unsupported, duplicate, or empty parameter")
		}
	}
	if values := params["sslmode"]; len(values) != 1 || values[0] != "verify-full" {
		return errors.New("poc DATABASE_URL requires explicit sslmode=verify-full")
	}
	for _, key := range []string{"sslrootcert", "sslcert", "sslkey"} {
		values := params[key]
		if len(values) != 1 || !filepath.IsAbs(values[0]) {
			return fmt.Errorf("poc DATABASE_URL requires an absolute %s path", key)
		}
	}
	return nil
}
