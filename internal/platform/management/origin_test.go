package management

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagementBrowserOriginConfigurationFailsClosed(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:3002", "http://0.0.0.0:3002",
		"http://127.0.0.2:3002", "https://127.0.0.2:3002",
		"http://127.0.0.1:3002/", "http://127.0.0.1:3002?key=1",
		"https://127.0.0.1:3443/", "https://127.0.0.1:3443#frag",
		"https://user:pw@127.0.0.1:3443", "https://127.0.0.1:65536",
		"https://localhost:3443", "https://evil.example:3443",
		"http://127.0.0.1:3002#token", "http://user:pw@127.0.0.1:3002",
		"http://127.0.0.1:0", "http://127.0.0.1:65536",
		"http://127.0.0.1:bad", "http://attacker.example:3002",
		"null", "*", "//127.0.0.1:3002",
	} {
		if err := validateBrowserOrigin(origin); err == nil {
			t.Errorf("unsafe management browser Origin accepted: %q", origin)
		}
	}
	for _, origin := range []string{
		"", "http://127.0.0.1:3002", "http://127.0.0.1:3033",
		"https://127.0.0.1:3443",
	} {
		if err := validateBrowserOrigin(origin); err != nil {
			t.Errorf("expected loopback Origin %q: %v", origin, err)
		}
	}
	verifier, idp := oidcFixture(t)
	defer idp.Close()
	for _, origin := range []string{"http://localhost:3002", "https://evil.example"} {
		if _, err := NewHandlerWithBrowserOrigin(&sql.DB{}, verifier, origin); err == nil {
			t.Errorf("configured unsafe browser origin %q", origin)
		}
	}
}

func TestManagementBrowserWriteOriginGateDeniesUntrustedOrigins(t *testing.T) {
	svc := &Service{AllowedBrowserOrigin: "http://127.0.0.1:3002"}
	for _, tc := range []struct {
		origin, site string
		want         bool
	}{
		{"", "", true}, // CLI, still subject to OIDC and project RBAC
		{"http://127.0.0.1:3002", "same-origin", true},
		{"http://127.0.0.1:3002", "cross-site", false},
		{"http://127.0.0.1:3003", "same-origin", false},
		{"https://attacker.example", "cross-site", false},
		{"null", "same-origin", false},
	} {
		request := httptest.NewRequest(http.MethodPut, "/api/v1/management/", nil)
		if tc.origin != "" {
			request.Header.Set("Origin", tc.origin)
		}
		if tc.site != "" {
			request.Header.Set("Sec-Fetch-Site", tc.site)
		}
		if got := svc.browserWriteAllowed(request); got != tc.want {
			t.Errorf("origin=%q site=%q: allowed=%t want=%t", tc.origin, tc.site, got, tc.want)
		}
	}
	duplicate := httptest.NewRequest(http.MethodDelete, "/api/v1/management/", nil)
	duplicate.Header.Add("Origin", "http://127.0.0.1:3002")
	duplicate.Header.Add("Origin", "http://127.0.0.1:3002")
	if svc.browserWriteAllowed(duplicate) {
		t.Fatal("multiple Origin headers were allowed")
	}
	tls := &Service{AllowedBrowserOrigin: "https://127.0.0.1:3443"}
	for _, tc := range []struct {
		origin  string
		allowed bool
	}{
		{"https://127.0.0.1:3443", true},
		{"http://127.0.0.1:3443", false},
		{"https://127.0.0.1:3002", false},
		{"https://localhost:3443", false},
		{"https://attacker.example", false},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/management/", nil)
		req.Header.Set("Origin", tc.origin)
		if got := tls.browserWriteAllowed(req); got != tc.allowed {
			t.Errorf("HTTPS Origin %q: allowed=%v, want=%v", tc.origin, got, tc.allowed)
		}
	}
	withoutOptIn := &Service{}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/management/", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3002")
	if withoutOptIn.browserWriteAllowed(req) {
		t.Fatal("unconfigured handler allowed browser write")
	}
}
