package httpapi

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func agentRequest(t *testing.T, method, path, body, identity string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if identity == "" {
		return req
	}
	u, err := url.Parse(identity)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		URIs:        []*url.URL{u},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
		VerifiedChains:   [][]*x509.Certificate{{leaf}},
	}
	return req
}

func protectedAgentRouter(t *testing.T) http.Handler {
	t.Helper()
	handler, err := AgentMTLSRouter(NewRouterWithAgentStore(agentstore.NewMemory()), "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

const clusterAIdentity = "spiffe://platform.example/cluster/cluster-a/agent"

func TestAgentMTLSRejectsMissingOrForgedIdentities(t *testing.T) {
	router := protectedAgentRouter(t)
	for _, tc := range []struct {
		name  string
		id    string
		alter func(*http.Request)
	}{
		{"no-cert", "", nil},
		{"different-trust-domain", "spiffe://bad.example/cluster/cluster-a/agent", nil},
		{"different-cluster", "spiffe://platform.example/cluster/cluster-b/agent", nil},
		{"admin-identity", "spiffe://platform.example/admin/cluster-a", nil},
		{"encoded-path", "spiffe://platform.example/cluster/%63luster-a/agent", nil},
		{"no-verified-chain", clusterAIdentity, func(r *http.Request) { r.TLS.VerifiedChains = nil }},
		{"not-a-client-cert", clusterAIdentity, func(r *http.Request) { r.TLS.PeerCertificates[0].ExtKeyUsage = nil }},
		{"certificate-authority", clusterAIdentity, func(r *http.Request) { r.TLS.PeerCertificates[0].IsCA = true }},
		{"untrusted-headers", "", func(r *http.Request) { r.Header.Set("X-Forwarded-Client-Cert", clusterAIdentity) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := agentRequest(t, http.MethodGet, "/api/v1/agent/desired?clusterId=cluster-a", "", tc.id)
			if tc.alter != nil {
				tc.alter(req)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized && !(tc.name == "different-cluster" && rec.Code == http.StatusForbidden) {
				t.Fatalf("status=%d, expected 401/403", rec.Code)
			}
		})
	}
}

func TestAgentMTLSRejectsBodyAndQueryScopeConfusion(t *testing.T) {
	router := protectedAgentRouter(t)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"wrong-query", "GET", "/api/v1/agent/desired?clusterId=cluster-b", "", 403},
		{"duplicate-query", "GET", "/api/v1/agent/desired?clusterId=cluster-a&clusterId=cluster-b", "", 400},
		{"wrong-report", "POST", "/api/v1/agent/report", `{"clusterId":"cluster-b","observations":[]}`, 403},
		{"wrong-lease", "POST", "/api/v1/agent/reconcile-lease/claim", `{"clusterId":"cluster-b","kind":"Workload"}`, 403},
		{"wrong-finalize", "POST", "/api/v1/agent/finalize-desired", `{"clusterId":"cluster-b"}`, 403},
		{"missing-cluster", "POST", "/api/v1/agent/heartbeat", `{"at":"2026-10-15T00:00:00Z"}`, 400},
		{"bad-json", "POST", "/api/v1/agent/report", "{oops", 400},
		{"oversized", "POST", "/api/v1/agent/report", strings.Repeat(" ", 1<<20+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, agentRequest(t, tc.method, tc.path, tc.body, clusterAIdentity))
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d", rec.Code, tc.want)
			}
		})
	}
}

func TestAgentMTLSOnlyAllowsAgentRoutes(t *testing.T) {
	router := protectedAgentRouter(t)
	for _, path := range []string{
		"/api/v1/internal/clusters/cluster-a/desired/Workload/train-1",
		"/api/v1/projects/project-a/binding",
		"/api/v1/compute-pools/pool-a",
		"/api/v1/clusters/cluster-a/status",
		"/api/v1/agent/not-real",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, agentRequest(t, "GET", path, "", clusterAIdentity))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status=%d want=404", path, rec.Code)
		}
	}
}

func TestAgentMTLSAuthorizedRegisterReachesHandler(t *testing.T) {
	router := protectedAgentRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, agentRequest(t, "POST", "/api/v1/agent/register",
		`{"clusterId":"cluster-a","agentVersion":"dev"}`, clusterAIdentity))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("authorized register failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, agentRequest(t, "GET",
		"/api/v1/agent/desired?clusterId=cluster-a", "", clusterAIdentity))
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized pull failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentMTLSRejectsInvalidTrustDomain(t *testing.T) {
	for _, domain := range []string{"", "://oops", "-bad.example", "bad..example"} {
		if _, err := AgentMTLSRouter(http.NotFoundHandler(), domain); err == nil {
			t.Errorf("accepted invalid trust domain %q", domain)
		}
	}
}

func TestAgentMTLSBodyIsPreservedForHandler(t *testing.T) {
	original := []byte(`{"clusterId":"cluster-a","agentVersion":"v1"}`)
	var received []byte
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = make([]byte, r.ContentLength)
		n, _ := r.Body.Read(received)
		received = received[:n]
		w.WriteHeader(http.StatusNoContent)
	})
	router, err := AgentMTLSRouter(inner, "platform.example")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, agentRequest(t, "POST", "/api/v1/agent/register", string(original), clusterAIdentity))
	if rec.Code != 204 || !bytes.Equal(received, original) {
		t.Fatalf("body changed, got=%q status=%d", received, rec.Code)
	}
}


func TestAgentMTLSRejectsAmbiguousJSONScope(t *testing.T) {
	router := protectedAgentRouter(t)
	for _, body := range []string{
		`{"clusterId":"cluster-b","clusterId":"cluster-a","agentVersion":"dev"}`,
		`{"clusterId":"cluster-b","ClusterId":"cluster-a","agentVersion":"dev"}`,
		`{"ClusterId":"cluster-a","agentVersion":"dev"}`,
		`{"clusterId":"cluster-a","agentVersion":"dev"}{"clusterId":"cluster-b"}`,
		`["cluster-a"]`,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, agentRequest(t, http.MethodPost, "/api/v1/agent/register", body, clusterAIdentity))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous cluster scope accepted: body=%q status=%d", body, rec.Code)
		}
	}
}
