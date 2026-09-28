package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func NewRouter() http.Handler {
	return NewRouterWithAgentStore(agentstore.NewMemory())
}

func NewRouterWithAgentStore(store agentstore.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	agentAPI := NewAgentAPI(store)
	mux.HandleFunc("POST /api/v1/agent/register", agentAPI.Register)
	mux.HandleFunc("POST /api/v1/agent/heartbeat", agentAPI.Heartbeat)
	mux.HandleFunc("GET /api/v1/agent/desired", agentAPI.Desired)
	mux.HandleFunc("POST /api/v1/agent/report", agentAPI.Report)

	desiredAPI := NewDesiredAPI(store)
	mux.HandleFunc("PUT /api/v1/clusters/{clusterID}/desired/{kind}/{resourceID}", desiredAPI.Upsert)
	mux.HandleFunc("DELETE /api/v1/clusters/{clusterID}/desired/{kind}/{resourceID}", desiredAPI.Delete)
	return mux
}
