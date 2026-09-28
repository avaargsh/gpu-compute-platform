package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	postgresstore "github.com/avaargsh/gpu-compute-platform/internal/store/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	store, cleanup := buildStore()
	defer cleanup()

	server := &http.Server{
		Addr:    ":8080",
		Handler: httpapi.NewRouterWithAgentStore(store),
	}

	log.Printf("control-plane listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func buildStore() (agentstore.Store, func()) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Print("DATABASE_URL is empty; using in-memory agent store")
		return agentstore.NewMemory(), func() {}
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	return postgresstore.New(db), func() {
		_ = db.Close()
	}
}
