// Management E2E fixture MUST NEVER be used as a production identity provider.
// It starts only under a hard TEST-ONLY environment gate and binds loopback.
// Playwright mocks the browser OAuth code flow; Go still introspects access
// tokens and uses the actual Management handler and ephemeral CI PostgreSQL.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"net/http/httptest"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/management"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func fail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// Refuse to grant test identities in any database other than the CI-only
// disposable browser database on loopback. An explicit test env flag alone
// is NOT enough to protect a user-configured production DSN.
func validateFixtureDSN(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		(u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") ||
		u.Port() != "5432" || u.Path != "/gpu_platform_browser_test" {
		return errors.New("fixture requires localhost:5432/gpu_platform_browser_test disposable database")
	}
	return nil
}

func main() {
	if os.Getenv("MANAGEMENT_E2E_TEST_ONLY") != "1" || os.Getenv("TEST_POSTGRES_DSN") == "" {
		log.Fatal("refusing to start test identity provider: MANAGEMENT_E2E_TEST_ONLY=1 and TEST_POSTGRES_DSN required")
	}
	if err := validateFixtureDSN(os.Getenv("TEST_POSTGRES_DSN")); err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("pgx", os.Getenv("TEST_POSTGRES_DSN"))
	fail(err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fail(db.PingContext(ctx))
	for _, file := range []string{"internal/store/postgres/schema.sql", "internal/platform/management/schema.sql"} {
		src, err := os.ReadFile(file)
		fail(err)
		_, err = db.ExecContext(ctx, string(src))
		fail(err)
	}
	// Names are scoped to an ephemeral CI database. No user or production
	// tables or grants are updated by this binary.
	for _, item := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO project_bindings(project_id,cluster_id,namespace) VALUES($1,$2,$3)", []any{"research", "browser-ci", "research"}},
		{"INSERT INTO cluster_bindings(pool_id,cluster_id,provider) VALUES($1,$2,'kueue')", []any{"pool-a", "browser-ci"}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,'operator')", []any{"https://issuer.example", "operator-alice", "research"}},
		{"INSERT INTO management_project_grants(issuer,subject,project_id,role) VALUES($1,$2,$3,'viewer')", []any{"https://issuer.example", "viewer-bob", "research"}},
	} {
		_, err = db.ExecContext(ctx, item.sql, item.args...)
		fail(err)
	}
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if r.Method != http.MethodPost || !ok || user != "fixture-client" || password != "fixture-secret" {
			http.Error(w, "invalid test introspection client", http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid test form", http.StatusBadRequest)
			return
		}
		subject := ""
		switch r.Form.Get("token") {
		case "e2e-access-token-operator":
			subject = "operator-alice"
		case "e2e-access-token-viewer":
			subject = "viewer-bob"
		}
		response := map[string]any{"active": subject != ""}
		if subject != "" {
			response["iss"] = "https://issuer.example"
			response["sub"] = subject
			response["aud"] = []string{"manager-api"}
			response["exp"] = time.Now().Add(time.Hour).Unix()
			response["token_type"] = "Bearer"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer idp.Close()
	handler, err := management.NewHandlerWithBrowserOrigin(db, management.Introspector{
		URL: idp.URL, Issuer: "https://issuer.example", Audience: "manager-api",
		ClientID: "fixture-client", ClientSecret: "fixture-secret", AllowHTTPForTests: true,
	}, "http://127.0.0.1:3002")
	fail(err)
	server := &http.Server{
		Addr: "127.0.0.1:8081", Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 20 * time.Second,
	}
	log.Print("TEST ONLY: local Management Go HTTP + PostgreSQL + simulated introspection ready on 127.0.0.1:8081")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
