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
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, errors.New("Agent mTLS client certificate or key cannot be loaded")
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS13,
				RootCAs:      pool,
				Certificates: []tls.Certificate{cert},
			},
		},
	}, nil
}
