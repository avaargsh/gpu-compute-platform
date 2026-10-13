package management

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// This test is opt-in: the keycloak job starts a disposable REAL HTTPS
// Keycloak service, not the httptest RFC7662 double used elsewhere.
// Test-only password grant obtains a genuine Keycloak access token;
// neither user passwords nor this grant are used by the production browser.
func TestKeycloakRealHTTPSIntrospectionAndRevocation(t *testing.T) {
	issuer := os.Getenv("KEYCLOAK_E2E_ISSUER")
	caPath := os.Getenv("KEYCLOAK_E2E_CA")
	if issuer == "" || caPath == "" {
		t.Skip("real disposable Keycloak fixture not configured")
	}
	if issuer != "https://127.0.0.1:8443/realms/gpu-management-e2e" {
		t.Fatal("refuse unknown issuer for disposable Keycloak integration")
	}
	pem, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("invalid Keycloak integration CA")
	}
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}
	endpoint := issuer + "/protocol/openid-connect/token"
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	call := func(target, id, secret string, form url.Values) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(id, secret)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return client.Do(req)
	}
	tokenResp, err := call(endpoint, "gpu-e2e-direct", "e2e-direct-secret-not-for-production", url.Values{
		"grant_type": {"password"}, "username": {"ci-operator"},
		"password": {"ci-disposable-password-not-for-production"},
		"scope": {"openid"},
	})
	if err != nil {
		t.Fatal("Keycloak test-token exchange transport:", err)
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusOK {
		// Never dump the body: provider responses may contain credentials.
		t.Fatalf("Keycloak test token exchange HTTP %d", tokenResp.StatusCode)
	}
	var tokenResult struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, 1<<16)).Decode(&tokenResult); err != nil {
		t.Fatal("Keycloak token response could not be parsed")
	}
	if len(tokenResult.AccessToken) < 32 || !strings.EqualFold(tokenResult.TokenType, "Bearer") {
		t.Fatal("Keycloak failed to issue a genuine bearer access token")
	}
	introspector := Introspector{
		URL: issuer + "/protocol/openid-connect/token/introspect",
		Issuer: issuer, Audience: "gpu-management-api",
		ClientID: "gpu-e2e-introspection",
		ClientSecret: "e2e-introspect-secret-not-for-production",
		Client: client,
	}
	identity, err := introspector.Verify(ctx, tokenResult.AccessToken)
	if err != nil {
		t.Fatalf("Keycloak real token rejected by strict Introspector: %v", err)
	}
	if identity.Issuer != issuer || identity.Subject == "" {
		t.Fatalf("unexpected real Keycloak identity issuer=%q subject-empty=%t", identity.Issuer, identity.Subject == "")
	}
	wrong := introspector
	wrong.Audience = "unintended-other-api"
	if _, err := wrong.Verify(ctx, tokenResult.AccessToken); err == nil {
		t.Fatal("real Keycloak token accepted by wrong API audience")
	}
	if _, err := introspector.Verify(ctx, "forged-not-an-issued-token"); err == nil {
		t.Fatal("non-issued bearer was accepted")
	}
	// Revoke the token using its TEST-ONLY issuing client, then query the
	// same Keycloak introspection endpoint. A revoked token must fail closed.
	revokeResp, err := call(issuer+"/protocol/openid-connect/revoke",
		"gpu-e2e-direct", "e2e-direct-secret-not-for-production",
		url.Values{"token": {tokenResult.AccessToken}, "token_type_hint": {"access_token"}})
	if err != nil {
		t.Fatal("Keycloak access-token revoke transport:", err)
	}
	defer revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("Keycloak access-token revoke HTTP %d", revokeResp.StatusCode)
	}
	_, err = introspector.Verify(ctx, tokenResult.AccessToken)
	if err == nil || errors.Is(err, ErrIntrospectionUnavailable) {
		t.Fatalf("Keycloak revoked access token was not explicitly denied (err=%v)", err)
	}
}
