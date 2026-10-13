package management

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIntrospectorRejectsTrailingAndOversizedResponse(t *testing.T) {
	const issuer = "https://idp.example.test/realms/gpu"
	valid := fmt.Sprintf(`{"active":true,"iss":%q,"sub":"alice","exp":%d,"aud":"gpu-management","token_type":"Bearer"}`, issuer, time.Now().Add(time.Hour).Unix())
	cases := []struct {
		name      string
		body      string
		wantValid bool
	}{
		{name: "valid bounded response", body: valid, wantValid: true},
		{name: "valid bounded whitespace", body: valid + " \n", wantValid: true},
		{name: "conflicting active false then true", body: strings.Replace(valid, `"active":true`, `"active":false,"active":true`, 1)},
		{name: "conflicting subject duplicate", body: strings.Replace(valid, `"sub":"alice"`, `"sub":"mallory","sub":"alice"`, 1)},
		{name: "case-insensitive active duplicate", body: strings.Replace(valid, `"active":true`, `"ACTIVE":false,"active":true`, 1)},
		{name: "duplicate unknown JSON key", body: strings.Replace(valid, `"sub":"alice"`, `"sub":"alice","vendor_hint":"one","vendor_hint":"two"`, 1)},
		{name: "nested extra unknown object allowed", body: strings.Replace(valid, `"sub":"alice"`, `"sub":"alice","vendor_extra":{"one":"a","two":"b"}`, 1), wantValid: true},
		{name: "trailing JSON object", body: valid + `{"active":false}`},
		{name: "trailing non-JSON bytes", body: valid + "not-json"},
		{name: "oversized valid prefix", body: valid + strings.Repeat(" ", 8193)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("expected introspection POST, got %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			introspector := Introspector{
				URL: server.URL, Issuer: issuer, Audience: "gpu-management",
				ClientID: "introspection-client", ClientSecret: "test-only-secret",
				AllowHTTPForTests: true,
			}
			got, err := introspector.Verify(context.Background(), "test-token-valid-123")
			if tc.wantValid {
				if err != nil || got != (Identity{Issuer: issuer, Subject: "alice"}) {
					t.Fatalf("valid provider response rejected: got=%+v err=%v", got, err)
				}
			} else if !errors.Is(err, ErrIntrospectionUnavailable) {
				t.Fatalf("must fail closed as provider-unavailable, got=%+v err=%v", got, err)
			}
		})
	}
}
