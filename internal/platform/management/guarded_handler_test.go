package management

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The live command uses this constructor rather than the unguarded local
// fixture. An authenticated mutation must not run under a DBA login.
func TestGuardedManagementHTTPRejectsDBALogin(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable TEST_POSTGRES_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var superuser bool
	if err := db.QueryRow("SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&superuser); err != nil {
		t.Fatal(err)
	}
	if !superuser {
		t.Skip("negative control requires the disposable CI DBA login")
	}
	auth, server := oidcFixture(t)
	defer server.Close()
	handler, err := NewGuardedHandlerWithBrowserOrigin(db, auth, "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut,
		"/api/v1/management/clusters/c/projects/p/resources/ComputePool/pool",
		strings.NewReader(`{"expectedGeneration":0,"spec":{}}`))
	request.Header.Set("Authorization", "Bearer good-access-token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", strings.Repeat("a", 32))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("privileged login mutation HTTP status=%d, want 503", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("security rejection response must not be cached")
	}
	// Liveness is intentionally separate: readiness/security acceptance
	// must never be inferred from this simple process health endpoint.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected a separate 204 process liveness check, got %d", rec.Code)
	}
}
