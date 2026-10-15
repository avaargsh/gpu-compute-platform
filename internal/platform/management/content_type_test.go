package management

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagementJSONMutationContentTypeParsing(t *testing.T) {
	for _, value := range []string{
		"application/json",
		"Application/JSON",
		"application/json; charset=utf-8",
		"application/json; charset=UTF-8",
		"application/json;charset=\"UTF-8\"",
	} {
		h := http.Header{}
		h.Set("Content-Type", value)
		if !validJSONMutationContentType(h) {
			t.Errorf("rejected valid JSON media type %q", value)
		}
	}
	for _, value := range []string{
		"",
		"text/plain",
		"application/jsonx",
		"application/json-patch+json",
		"application/json, text/plain",
		"application/json; charset=iso-8859-1",
		"application/json; profile=evil",
		"application/json; charset=UTF-8; charset=latin1",
		"application/json;",
	} {
		h := http.Header{}
		if value != "" {
			h.Set("Content-Type", value)
		}
		if validJSONMutationContentType(h) {
			t.Errorf("accepted invalid JSON media type %q", value)
		}
	}
	duplicate := http.Header{}
	duplicate.Add("Content-Type", "application/json")
	duplicate.Add("Content-Type", "application/json")
	if validJSONMutationContentType(duplicate) {
		t.Fatal("accepted duplicate JSON Content-Type headers")
	}
}

func TestManagementJSONMutationRejectsInvalidHeaderBeforeDatabaseWrite(t *testing.T) {
	auth, idp := oidcFixture(t)
	defer idp.Close()
	// An intentionally unusable DB ensures invalid media types cannot reach
	// the mutation's authorization/transaction path.
	handler, err := NewHandler(&sql.DB{}, auth)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/management/clusters/cluster-a/projects/project-a/resources/Workload/job-a"
	for _, tc := range []struct {
		name  string
		types []string
	}{
		{name: "missing"},
		{name: "prefix-smuggling", types: []string{"application/jsonx"}},
		{name: "suffix", types: []string{"application/json-patch+json"}},
		{name: "comma-list", types: []string{"application/json, text/plain"}},
		{name: "unsupported-charset", types: []string{"application/json; charset=iso-8859-1"}},
		{name: "unknown-parameter", types: []string{"application/json; profile=unexpected"}},
		{name: "duplicate", types: []string{"application/json", "application/json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"expectedGeneration":0,"spec":{}}`))
			req.Header.Set("Authorization", "Bearer good-access-token")
			req.Header.Set("Idempotency-Key", strings.Repeat("a", 32))
			for _, ct := range tc.types {
				req.Header.Add("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status=%d want 415 for %q", rec.Code, tc.types)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("rejection must not be cached")
			}
		})
	}
}
