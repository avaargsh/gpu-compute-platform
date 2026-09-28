package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	postgresstore "github.com/avaargsh/gpu-compute-platform/internal/store/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	store, placement, cleanup := buildStore()
	defer cleanup()

	server := &http.Server{
		Addr:    ":8080",
		Handler: httpapi.NewRouterWithDependencies(store, placement),
	}

	log.Printf("control-plane listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildStore() (agentstore.Store, httpapi.PlacementResolver, func()) {
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
