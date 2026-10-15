package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/bootstrap"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	postgresstore "github.com/avaargsh/gpu-compute-platform/internal/store/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg, err := bootstrap.Load(os.Getenv)
	if err != nil {
		log.Fatalf("unsafe control-plane startup configuration: %v", err)
	}
	agentListener, err := bootstrap.LoadAgentListener(os.Getenv, cfg.Profile)
	if err != nil {
		log.Fatalf("unsafe Agent HTTPS configuration: %v", err)
	}
	store, placement, cleanup := buildStore(cfg.DatabaseURL)
	defer cleanup()

	handler := httpapi.NewRouterWithDependencies(store, placement)
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}
	if agentListener.Enabled {
		// This listener only exposes the Agent protocol and /healthz.
		// User/admin paths stay disabled until OIDC/RBAC is independently
		// implemented. VerifiedChains comes from Go's TLS handshake.
		handler, err = httpapi.AgentMTLSRouter(handler, agentListener.TrustDomain)
		if err != nil {
			log.Fatalf("Agent identity router: %v", err)
		}
		server.Handler = handler
		server.TLSConfig, err = bootstrap.AgentTLSConfig(agentListener.ClientCA)
		if err != nil {
			log.Fatalf("Agent client CA: %v", err)
		}
		log.Printf("cluster-agent mTLS-only control plane listening on %s", server.Addr)
		err = server.ListenAndServeTLS(agentListener.ServerCert, agentListener.ServerKey)
	} else {
		log.Printf("development control plane listening on %s", server.Addr)
		err = server.ListenAndServe()
	}
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildStore(dsn string) (agentstore.Store, httpapi.BindingStore, func()) {
	if dsn == "" {
		log.Print("DATABASE_URL is empty; using in-memory agent store")
		return agentstore.NewMemory(), httpapi.NewMemoryPlacementResolver(), func() {}
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		log.Fatalf("ping postgres: %v", err)
	}
	store := postgresstore.New(db)
	return store, store, func() {
		_ = db.Close()
	}
}
