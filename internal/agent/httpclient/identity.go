package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Match the cluster segment admitted by the server-side Agent mTLS router.
// This is defense in depth: server TLS verification and the configured
// trust-domain/cluster authorization remain authoritative.
var clientClusterPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// NewClusterMTLSClient binds the Agent's on-disk mTLS identity to the
// configured CLUSTER_ID at startup and on every certificate reload.
// It does not issue, renew or revoke certificates, or replace server-side
// verified-chain and SPIFFE trust-domain checks.
func NewClusterMTLSClient(caFile, certFile, keyFile, clusterID string) (*http.Client, error) {
	if !clientClusterPattern.MatchString(clusterID) {
		return nil, errors.New("invalid expected cluster identity")
	}
	client, err := NewMTLSClient(caFile, certFile, keyFile)
	if err != nil {
		return nil, err
	}
	// A malformed or wrong-cluster identity is a startup error, not an
	// endless background retry after the Agent starts watching Kubernetes.
	initial, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil || !certificateMatchesCluster(&initial, clusterID) {
		return nil, errors.New("Agent mTLS identity does not match configured cluster")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.GetClientCertificate == nil {
		return nil, errors.New("Agent mTLS credential reload is not configured")
	}
	load := transport.TLSClientConfig.GetClientCertificate
	transport.TLSClientConfig.GetClientCertificate = func(info *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		pair, err := load(info)
		if err != nil {
			return nil, err
		}
		if !certificateMatchesCluster(pair, clusterID) {
			// Do not fall back to a previously accepted certificate.
			return nil, errors.New("Agent mTLS rotated identity rejected")
		}
		return pair, nil
	}
	return client, nil
}

func certificateMatchesCluster(pair *tls.Certificate, clusterID string) bool {
	if pair == nil || len(pair.Certificate) == 0 {
		return false
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA || len(leaf.URIs) != 1 {
		return false
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return false
	}
	clientAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			clientAuth = true
			break
		}
	}
	if !clientAuth {
		return false
	}
	uri := leaf.URIs[0]
	if uri == nil || uri.Scheme != "spiffe" || uri.Host == "" ||
		strings.Contains(uri.Host, ":") || uri.User != nil ||
		uri.RawQuery != "" || uri.Fragment != "" || uri.RawFragment != "" ||
		uri.Opaque != "" {
		return false
	}
	// URI path must be literal, not percent-encoded or noncanonical.
	path := "/cluster/" + clusterID + "/agent"
	return uri.Path == path && uri.EscapedPath() == path &&
		uri.String() == (&url.URL{Scheme: "spiffe", Host: uri.Host, Path: path}).String()
}
