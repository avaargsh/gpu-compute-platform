package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"time"
)

// NewMTLSClient uses standard-library TLS verification and a dedicated
// CA to authenticate the control plane. No InsecureSkipVerify, custom
// CA bypass or bearer-token fallback is permitted for Agent mTLS.
//
// Client identity files are reloaded for each new TLS handshake so an
// operator can rotate the short-lived Agent certificate/key without
// restarting the process. HTTP keep-alives are disabled deliberately: a
// previously authenticated connection must not keep using an old identity
// after the filesystem's certificate has changed. CA trust changes still
// require a controlled restart.
func NewMTLSClient(caFile, certFile, keyFile string) (*http.Client, error) {
	if caFile == "" || certFile == "" || keyFile == "" {
		return nil, errors.New("Agent mTLS requires CA, client certificate and private key paths")
	}
	pemCA, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errors.New("Agent mTLS CA file cannot be read")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemCA) {
		return nil, errors.New("Agent mTLS CA contains no valid certificates")
	}
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, errors.New("Agent mTLS client certificate or key cannot be loaded")
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// A fresh handshake on every request is intentional: otherwise
			// HTTP/1.1 can reuse a TLS session authenticated with an old cert.
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    pool,
				GetClientCertificate: func(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
					cert, err := tls.LoadX509KeyPair(certFile, keyFile)
					if err != nil {
						// Never return a cached certificate on rotation failure.
						return nil, errors.New("Agent mTLS client certificate reload failed")
					}
					return &cert, nil
				},
			},
		},
	}, nil
}
