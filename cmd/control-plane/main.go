package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
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
	if operatorPreviewConfigured() {
		// A loopback browser preview cannot share the Agent-only mTLS router.
		if agentListener.Enabled && !agentListener.Separate {
			log.Fatal("operator preview cannot coexist with the legacy Agent-only mTLS listener")
		}
		if !isLoopbackAddress(cfg.ListenAddr) {
			log.Fatal("operator preview requires an explicit loopback listener")
		}
		handler, err = httpapi.NewRouterWithOperatorPreview(store, placement, httpapi.OperatorPreview{
			Token:                 os.Getenv("PORTAL_OPERATOR_TOKEN"),
			ClusterID:             domain.ID(os.Getenv("PORTAL_OPERATOR_CLUSTER_ID")),
			Projects:              strings.Split(os.Getenv("PORTAL_OPERATOR_PROJECT_IDS"), ","),
			DevelopmentWriteToken: os.Getenv("PORTAL_OPERATOR_WRITE_TOKEN"),
			DevelopmentWrites:     cfg.Profile == "development",
		})
		if err != nil {
			log.Fatalf("operator preview configuration invalid: %v", err)
		}
	}
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}
	if agentListener.Enabled {
		// This handler exposes only the Agent protocol and /healthz. It is a
		// separate handler instance so the ordinary loopback API never inherits
		// the Agent identity gate or becomes reachable through the mTLS port.
		agentHandler, routerErr := httpapi.AgentMTLSRouter(handler, agentListener.TrustDomain)
		if routerErr != nil {
			log.Fatalf("Agent identity router: %v", routerErr)
		}
		tlsConfig, tlsErr := bootstrap.AgentTLSConfig(agentListener.ClientCA)
		if tlsErr != nil {
			log.Fatalf("Agent client CA: %v", tlsErr)
		}
		if agentListener.Separate {
			agentServer := &http.Server{
				Addr:      agentListener.ListenAddr,
				Handler:   agentHandler,
				TLSConfig: tlsConfig,
			}
			go func() {
				log.Printf("cluster-agent mTLS-only listener listening on %s", agentServer.Addr)
				listenerErr := agentServer.ListenAndServeTLS(agentListener.ServerCert, agentListener.ServerKey)
				if listenerErr != nil && listenerErr != http.ErrServerClosed {
					log.Fatalf("cluster-agent mTLS listener failed: %v", listenerErr)
				}
			}()
			log.Printf("development control plane listening on %s", server.Addr)
			err = server.ListenAndServe()
		} else {
			// Preserve the original PoC mode: one mTLS-only listener, with no
			// user/admin/internal route exposure.
			server.Handler = agentHandler
			server.TLSConfig = tlsConfig
			log.Printf("cluster-agent mTLS-only control plane listening on %s", server.Addr)
			err = server.ListenAndServeTLS(agentListener.ServerCert, agentListener.ServerKey)
		}
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

func operatorPreviewConfigured() bool {
	return os.Getenv("PORTAL_OPERATOR_TOKEN") != "" ||
		os.Getenv("PORTAL_OPERATOR_CLUSTER_ID") != "" ||
		os.Getenv("PORTAL_OPERATOR_PROJECT_IDS") != "" ||
		os.Getenv("PORTAL_OPERATOR_WRITE_TOKEN") != ""
}

func isLoopbackAddress(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return false
	}
	return host == "127.0.0.1" || host == "::1"
}
