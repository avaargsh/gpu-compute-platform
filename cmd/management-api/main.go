package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/management"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// A *separate* management process never registers the old, unauthenticated
// Cluster Agent/desired/binding routes. Bind to loopback behind a trusted
// TLS reverse proxy until end-to-end network policy is reviewed.
func main() {
	dsn := os.Getenv("MANAGEMENT_DATABASE_URL")
	if dsn == "" {
		log.Fatal("MANAGEMENT_DATABASE_URL is required; memory writes forbidden")
	}
	addr := os.Getenv("MANAGEMENT_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8081"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		log.Fatal("management API must bind loopback")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal("management postgres unavailable: ", err)
	}
	var ready bool
	err = db.QueryRowContext(ctx, `SELECT
 to_regclass('management_project_grants') IS NOT NULL AND
 to_regclass('management_operation_audit') IS NOT NULL AND
 to_regclass('desired_resources') IS NOT NULL`).Scan(&ready)
	if err != nil || !ready {
		log.Fatal("management schema missing; apply migrations with DBA before startup")
	}
	auth := management.Introspector{
		URL:          os.Getenv("OIDC_INTROSPECTION_URL"),
		Issuer:       os.Getenv("OIDC_ISSUER"),
		Audience:     os.Getenv("OIDC_AUDIENCE"),
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
	}
	// Only the explicitly configured loopback browser Origin may issue
	// authenticated mutations through the local Vite proxy.
	handler, err := management.NewHandlerWithBrowserOrigin(db, auth, os.Getenv("MANAGEMENT_BROWSER_ORIGIN"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("scoped management API listening on %s; no Agent API registered", addr)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
