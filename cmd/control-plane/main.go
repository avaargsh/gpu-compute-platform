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
	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	postgresstore "github.com/avaargsh/gpu-compute-platform/internal/store/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	store, placement, cleanup := buildStore()
	defer cleanup()

	handler := httpapi.NewRouterWithDependencies(store, placement)
	if operatorPreviewConfigured() {
		if !isLoopbackAddress(listenAddress()) {
			log.Fatal("operator preview requires CONTROL_PLANE_LISTEN_ADDR=127.0.0.1:8080")
		}
		var err error
		handler, err = httpapi.NewRouterWithOperatorPreview(store, placement, httpapi.OperatorPreview{
			Token:     os.Getenv("PORTAL_OPERATOR_TOKEN"),
			ClusterID: domain.ID(os.Getenv("PORTAL_OPERATOR_CLUSTER_ID")),
			Projects:  strings.Split(os.Getenv("PORTAL_OPERATOR_PROJECT_IDS"), ","),
		})
		if err != nil {
			log.Fatalf("operator preview configuration invalid: %v", err)
		}
	}
	server := &http.Server{
		Addr:    listenAddress(),
		Handler: handler,
	}

	log.Printf("control-plane listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildStore() (agentstore.Store, httpapi.BindingStore, func()) {
	dsn := os.Getenv("DATABASE_URL")
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

// listenAddress keeps the existing production default, while allowing a
// loopback-only listener for the unauthenticated local Portal preview.
func listenAddress() string {
	if addr := os.Getenv("CONTROL_PLANE_LISTEN_ADDR"); addr != "" {
		return addr
	}
	return ":8080"
}

func operatorPreviewConfigured() bool {
	return os.Getenv("PORTAL_OPERATOR_TOKEN") != "" ||
		os.Getenv("PORTAL_OPERATOR_CLUSTER_ID") != "" ||
		os.Getenv("PORTAL_OPERATOR_PROJECT_IDS") != ""
}

func isLoopbackAddress(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return false
	}
	return host == "127.0.0.1" || host == "::1"
}
