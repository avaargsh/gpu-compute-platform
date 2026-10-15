package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntrospectionValidatesRemoteAuthorityAndAccessToken(t *testing.T) {
	expires := time.Now().Add(time.Hour).Unix()
	response := fmt.Sprintf(`{"active":true,"sub":"user-1","iss":"https://issuer.example","aud":["another","gpu-api"],"exp":%d}`, expires)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		id, secret, ok := r.BasicAuth()
		if !ok || id != "gpu-control-plane" || secret != "private-secret" {
			t.Errorf("introspection client auth failed")
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("token") != "actual-access-token" {
			t.Errorf("token was not submitted by server-side POST")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer provider.Close()
	verifier, err := NewIntrospector(provider.URL, "https://issuer.example", "gpu-api", "gpu-control-plane", "private-secret", provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(context.Background(), "actual-access-token")
	if err != nil || principal.Subject != "user-1" || principal.Issuer != "https://issuer.example" {
		t.Fatalf("principal=%#v err=%v", principal, err)
	}
	for _, token := range []string{"", "abc def", "x\nINJECT", strings.Repeat("x", 8193)} {
		if _, err := verifier.Verify(context.Background(), token); err == nil {
			t.Errorf("accepted invalid bearer token size/format")
		}
	}
}

func TestIntrospectionRejectsUntrustedAnswers(t *testing.T) {
	expires := time.Now().Add(time.Hour).Unix()
	old := time.Now().Add(-time.Hour).Unix()
	tests := []struct {
		name string
		body string
	}{
		{"inactive", fmt.Sprintf(`{"active":false,"sub":"u","aud":"gpu-api","exp":%d}`, expires)},
		{"wrong-issuer", fmt.Sprintf(`{"active":true,"sub":"u","iss":"https://evil.example","aud":"gpu-api","exp":%d}`, expires)},
		{"wrong-audience", fmt.Sprintf(`{"active":true,"sub":"u","aud":["unrelated"],"exp":%d}`, expires)},
		{"expired", fmt.Sprintf(`{"active":true,"sub":"u","aud":"gpu-api","exp":%d}`, old)},
		{"missing-exp", `{"active":true,"sub":"u","aud":"gpu-api"}`},
		{"missing-sub", fmt.Sprintf(`{"active":true,"aud":"gpu-api","exp":%d}`, expires)},
		{"future-not-before", fmt.Sprintf(`{"active":true,"sub":"u","aud":"gpu-api","exp":%d,"nbf":%d}`, expires, expires)},
		{"not-json", `{"active":true,`},
		{"oversize", strings.Repeat("x", 65537)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			v, err := NewIntrospector(server.URL, "https://issuer.example", "gpu-api", "id", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Verify(context.Background(), "token"); err == nil {
				t.Fatal("invalid introspection response granted identity")
			}
		})
	}
}

func TestIntrospectionRejectsInsecureEndpointAndRedirect(t *testing.T) {
	for _, endpoint := range []string{"http://issuer.example/introspect", "https://u:p@issuer.example/introspect", "https://issuer.example/?token=secret", "file:///tmp/idp"} {
		if _, err := NewIntrospector(endpoint, "https://issuer.example", "gpu-api", "id", "secret", nil); err == nil {
			t.Errorf("insecure introspection URL accepted: %s", endpoint)
		}
	}
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("token reached redirected endpoint")
		w.WriteHeader(200)
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	v, err := NewIntrospector(redirect.URL, "https://issuer.example", "gpu-api", "id", "secret", redirect.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(context.Background(), "token"); err == nil {
		t.Fatal("redirect response accepted")
	}
}

func TestAudienceVariants(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok bool
	}{
		{`"gpu-api"`, true}, {`["other","gpu-api"]`, true},
		{`"other"`, false}, {`["other"]`, false},
		{`{"role":"gpu-api"}`, false}, {`null`, false}, {`[]`, false},
	} {
		if matchesAudience([]byte(tc.raw), "gpu-api") != tc.ok {
			t.Errorf("audience %s accepted=%t", tc.raw, !tc.ok)
		}
	}
}
