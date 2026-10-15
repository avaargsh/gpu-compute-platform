package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/bootstrap"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/authn"
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
	humanListener, err := bootstrap.LoadHumanListener(os.Getenv, cfg.Profile)
	if err != nil {
		log.Fatalf("unsafe human HTTPS configuration: %v", err)
	}
	if humanListener.Enabled && agentListener.Enabled {
		log.Fatal("human OAuth and Agent mTLS must not share the same listener")
	}
	store, placement, cleanup := buildStore(cfg.DatabaseURL)
	defer cleanup()

	handler := httpapi.NewRouterWithDependencies(store, placement)
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}
	if humanListener.Enabled {
		secret, err := humanListener.ReadClientSecret()
		if err != nil {
			log.Fatalf("OAuth client credential: %v", err)
		}
		verifier, err := authn.NewIntrospector(
			humanListener.Endpoint, humanListener.Issuer,
			humanListener.Audience, humanListener.ClientID, secret, nil,
		)
		if err != nil {
			log.Fatalf("OAuth introspection: %v", err)
		}
		// This PoC serves ONLY project-scoped GET and healthz. No Agent,
		// administrative, global resource or mutation route is reachable.
		membership, ok := store.(httpapi.ProjectMembership)
		if !ok {
			log.Fatal("project-scoped API requires a PostgreSQL-backed membership store")
		}
		handler, err = httpapi.NewProjectReadRouter(store, placement, membership, verifier)
		if err != nil {
			log.Fatalf("project read API: %v", err)
		}
		server.Handler = handler
		log.Printf("project-scoped HTTPS read API listening on %s", server.Addr)
		err = server.ListenAndServeTLS(humanListener.ServerCert, humanListener.ServerKey)
	} else if agentListener.Enabled {
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
