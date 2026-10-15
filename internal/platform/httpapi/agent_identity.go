package httpapi

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// AgentMTLSRouter restricts its handler to the cluster-agent protocol.
// The actual TLS server MUST verify clients against a separately trusted
// client CA; X-Forwarded-Client-Cert and other headers are never trusted.
// It intentionally DOES NOT grant access to human, admin or internal routes.
func AgentMTLSRouter(next http.Handler, trustDomain string) (http.Handler, error) {
	if next == nil || !validTrustDomain(trustDomain) {
		return nil, errors.New("valid Agent mTLS trust domain and handler are required")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !agentProtocolRoute(r) {
			http.NotFound(w, r)
			return
		}
		clusterID, ok := verifiedAgentCluster(r, trustDomain)
		if !ok {
			http.Error(w, "unauthenticated cluster agent", http.StatusUnauthorized)
			return
		}
		requestedID, status := requestClusterID(w, r)
		if status != 0 {
			http.Error(w, "invalid cluster agent request", status)
			return
		}
		if requestedID != clusterID {
			http.Error(w, "cluster agent scope mismatch", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}

var agentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var trustDomainPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,251}[a-zA-Z0-9]$`)

func validTrustDomain(v string) bool {
	return trustDomainPattern.MatchString(v) && !strings.Contains(v, "..") &&
		!strings.HasPrefix(v, "-") && !strings.Contains(v, "://")
}

func agentProtocolRoute(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/agent/desired",
		"POST /api/v1/agent/register",
		"POST /api/v1/agent/heartbeat",
		"POST /api/v1/agent/report",
		"POST /api/v1/agent/reconcile-lease/claim",
		"POST /api/v1/agent/reconcile-lease/release",
		"POST /api/v1/agent/finalize-desired":
		return true
	default:
		return false
	}
}

func verifiedAgentCluster(r *http.Request, trustDomain string) (string, bool) {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		return "", false
	}
	leaf := r.TLS.PeerCertificates[0]
	if leaf == nil || leaf.IsCA || len(leaf.URIs) != 1 ||
		!hasClientAuthUsage(leaf) {
		return "", false
	}
	u := leaf.URIs[0]
	if u == nil || u.Scheme != "spiffe" || u.Host != trustDomain ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		u.RawFragment != "" || u.Opaque != "" {
		return "", false
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "cluster" ||
		parts[3] != "agent" || !agentIDPattern.MatchString(parts[2]) ||
		u.EscapedPath() != "/cluster/"+parts[2]+"/agent" {
		return "", false
	}
	return parts[2], true
}

func hasClientAuthUsage(cert *x509.Certificate) bool {
	for _, usage := range cert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			return true
		}
	}
	return false
}

func requestClusterID(w http.ResponseWriter, r *http.Request) (string, int) {
	if r.Method == http.MethodGet {
		values := r.URL.Query()["clusterId"]
		if len(values) != 1 || !agentIDPattern.MatchString(values[0]) {
			return "", http.StatusBadRequest
		}
		return values[0], 0
	}
	// Cap and replay the body so authorization and handlers parse identical
	// bytes. Do not consume unbounded JSON and do not log request bodies.
	if r.Body == nil {
		return "", http.StatusBadRequest
	}
	const maxBodyBytes = 1 << 20
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return "", http.StatusRequestEntityTooLarge
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	clusterID, valid := canonicalAgentClusterID(body)
	if !valid {
		return "", http.StatusBadRequest
	}
	return clusterID, 0
}

// canonicalAgentClusterID ensures that the identity used for authorization
// cannot differ from the clusterId decoded by downstream Agent handlers.
// encoding/json otherwise accepts duplicate and case-insensitive field names.
func canonicalAgentClusterID(body []byte) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return "", false
	}
	seen := make(map[string]struct{})
	var clusterID string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return "", false
		}
		key, ok := token.(string)
		if !ok {
			return "", false
		}
		normalized := strings.ToLower(key)
		if _, exists := seen[normalized]; exists {
			return "", false
		}
		seen[normalized] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", false
		}
		if key == "clusterId" {
			if err := json.Unmarshal(value, &clusterID); err != nil {
				return "", false
			}
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return "", false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", false
	}
	return clusterID, agentIDPattern.MatchString(clusterID)
}

// The HTTP server verifies certificate chains and the client-usage EKU
// during TLS negotiation. This additional route check always requires
// r.TLS.VerifiedChains, not merely PeerCertificates or forwarded headers.
