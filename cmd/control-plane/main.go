package main

import (
	"log"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
)

func main() {
	server := &http.Server{
		Addr:    ":8080",
		Handler: httpapi.NewRouter(),
	}

	log.Printf("control-plane listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
