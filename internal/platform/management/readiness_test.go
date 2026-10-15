package management

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// A running process must not be promoted on /healthz alone. Readiness uses
// the same scoped runtime login authority checks as guarded writes.
func TestManagementReadinessFailsClosedWhenDatabaseIsUnavailable(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://missing:missing@127.0.0.1:1/unavailable?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth, idp := oidcFixture(t)
	defer idp.Close()
	for _, guard := range []bool{false, true} {
		handler, err := newHandlerWithAuthorityCheck(db, auth, "", guard)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("liveness guard=%v status=%d want 204", guard, response.Code)
		}
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("readiness guard=%v status=%d want 503", guard, response.Code)
		}
		if response.Header().Get("Cache-Control") != "no-store" ||
			response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("readiness response must prevent cache and type sniffing")
		}
		body, _ := io.ReadAll(response.Body)
		if strings.Contains(string(body), "postgres://") ||
			strings.Contains(string(body), "missing:missing") ||
			strings.Contains(string(body), "127.0.0.1:1") {
			t.Fatal("readiness response leaked backend database information")
		}
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/readyz", nil))
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST readiness status=%d, want 405", response.Code)
		}
	}
}

func TestManagementReadinessRejectsDBALoginEvenWhenDatabaseAvailable(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires the disposable CI database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var isSuperuser bool
	if err := db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&isSuperuser); err != nil {
		t.Fatal(err)
	}
	if !isSuperuser {
		t.Skip("negative control requires the disposable DBA login")
	}
	auth, idp := oidcFixture(t)
	defer idp.Close()
	h, err := NewGuardedHandlerWithBrowserOrigin(db, auth, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		want := http.StatusNoContent
		if path == "/readyz" {
			want = http.StatusServiceUnavailable
		}
		if response.Code != want {
			t.Fatalf("%s status=%d, want %d", path, response.Code, want)
		}
	}
}
