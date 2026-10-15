package bootstrap

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// AgentTLSConfig configures the Go HTTPS listener to verify any presented
// client certificate using a dedicated Agent issuing CA. Agent-only routes
// separately reject requests with no verified certificate and require a
// cluster-specific URI SAN. The TLS certificate for this server must also
// be supplied to http.Server.ListenAndServeTLS.
func AgentTLSConfig(clientCAFile string) (*tls.Config, error) {
	if clientCAFile == "" {
		return nil, errors.New("Agent client CA path is required")
	}
	data, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, errors.New("unable to read Agent client CA file")
	}
	cas := x509.NewCertPool()
	if !cas.AppendCertsFromPEM(data) {
		return nil, errors.New("Agent client CA file contains no certificates")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  cas,
	}, nil
}
