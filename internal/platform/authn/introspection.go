// Package authn uses RFC 7662 token introspection rather than trusting JWT
// contents supplied by a browser. It does not implement OIDC JWT/JWKS validation.
package authn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Principal struct {
	Issuer  string
	Subject string
}

type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

type Introspector struct {
	endpoint string
	issuer   string
	audience string
	clientID string
	secret   string
	client   *http.Client
}

func NewIntrospector(endpoint, issuer, audience, clientID, clientSecret string, client *http.Client) (*Introspector, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
		u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" ||
		issuer == "" || audience == "" || clientID == "" || clientSecret == "" {
		return nil, errors.New("introspection requires HTTPS endpoint, trusted issuer, audience and client credentials")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	// Make sure a compromised identity endpoint cannot redirect an access token
	// to an untrusted service; do not mutate the caller's client.
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if local.Timeout <= 0 || local.Timeout > 10*time.Second {
		local.Timeout = 5 * time.Second
	}
	return &Introspector{endpoint: endpoint, issuer: issuer, audience: audience, clientID: clientID, secret: clientSecret, client: &local}, nil
}

func (v *Introspector) Verify(ctx context.Context, token string) (Principal, error) {
	denied := errors.New("access token is not valid")
	if len(token) == 0 || len(token) > 8192 || strings.ContainsAny(token, " \t\r\n") {
		return Principal{}, denied
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Principal{}, denied
	}
	req.SetBasicAuth(v.clientID, v.secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return Principal{}, denied
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Principal{}, denied
	}
	const maxResponse = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return Principal{}, denied
	}
	var info struct {
		Active bool            `json:"active"`
		Sub    string          `json:"sub"`
		Iss    string          `json:"iss"`
		Aud    json.RawMessage `json:"aud"`
		Exp    json.Number     `json:"exp"`
		Nbf    json.Number     `json:"nbf"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&info); err != nil || !info.Active ||
		len(info.Sub) == 0 || len(info.Sub) > 256 ||
		(info.Iss != "" && info.Iss != v.issuer) ||
		!matchesAudience(info.Aud, v.audience) {
		return Principal{}, denied
	}
	exp, err := strconv.ParseInt(string(info.Exp), 10, 64)
	if err != nil || exp <= time.Now().Unix() {
		return Principal{}, denied
	}
	if info.Nbf != "" {
		nbf, err := strconv.ParseInt(string(info.Nbf), 10, 64)
		if err != nil || nbf > time.Now().Unix() {
			return Principal{}, denied
		}
	}
	// Issuer is pinned to the configured HTTPS introspection authority.
	return Principal{Issuer: v.issuer, Subject: info.Sub}, nil
}

func matchesAudience(raw json.RawMessage, expected string) bool {
	if len(raw) == 0 {
		return false
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}
	var multiple []string
	if json.Unmarshal(raw, &multiple) != nil {
		return false
	}
	for _, candidate := range multiple {
		if candidate == expected {
			return true
		}
	}
	return false
}
