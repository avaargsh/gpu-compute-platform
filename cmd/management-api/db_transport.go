package main

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// validateManagementDatabaseTransport is the mandatory production API
// PostgreSQL connection contract. The local browser test fixture is a
// different binary and intentionally uses an ephemeral loopback database.
//
// Reject weak/downgrade SSL modes and alternative connection targets before
// any network attempt. Never include the DSN or password in an error.
func validateManagementDatabaseTransport(raw string) error {
	const invalid = "MANAGEMENT_DATABASE_URL requires an explicit PostgreSQL URI, database, login and verified TLS"
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		u.Opaque != "" || u.Host == "" || u.Hostname() == "" ||
		strings.ContainsAny(u.Hostname(), ",/") || u.User == nil ||
		u.User.Username() == "" || u.Path == "" || u.Path == "/" ||
		u.Fragment != "" {
		return errors.New(invalid)
	}

	// URI query parsing must be strict: libpq-compatible parsers may choose
	// different values for duplicated parameters. No security-critical
	// parameter can be shadowed by a later or earlier occurrence.
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(params["sslmode"]) != 1 ||
		params.Get("sslmode") != "verify-full" ||
		len(params["sslrootcert"]) != 1 ||
		params.Get("sslrootcert") == "" {
		return errors.New("MANAGEMENT_DATABASE_URL must explicitly use sslmode=verify-full and one sslrootcert")
	}
	for _, override := range []string{"host", "hostaddr", "service", "servicefile"} {
		if _, supplied := params[override]; supplied {
			return errors.New("MANAGEMENT_DATABASE_URL must not override the validated server address")
		}
	}
	// Require an absolute CA path so PGHOME/working-directory differences
	// cannot silently change which trust root is loaded at runtime.
	rootCert := params.Get("sslrootcert")
	if !filepath.IsAbs(rootCert) {
		return errors.New("MANAGEMENT_DATABASE_URL sslrootcert must use an absolute CA path")
	}
	info, err := os.Stat(rootCert)
	if err != nil || info.IsDir() {
		return errors.New("MANAGEMENT_DATABASE_URL CA trust file is missing or inaccessible")
	}
	return nil
}
