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
	return NewRouterWithDependencies(store, NewMemoryPlacementResolver())
}

func NewRouterWithDependencies(store agentstore.Store, bindings BindingStore) http.Handler {
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

	bindingAPI := NewBindingAPI(bindings)
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/binding", bindingAPI.UpsertProject)
	mux.HandleFunc("PUT /api/v1/compute-pools/{poolID}/binding", bindingAPI.UpsertPool)

	if migrations, ok := bindings.(MigrationStore); ok {
		migrationAPI := NewMigrationAPI(migrations, store)
		mux.HandleFunc("POST /api/v1/compute-pools/{poolID}/migrations", migrationAPI.Create)
		mux.HandleFunc("GET /api/v1/compute-pools/{poolID}/migrations/{migrationID}", migrationAPI.Get)
		mux.HandleFunc("PUT /api/v1/compute-pools/{poolID}/migrations/{migrationID}/status", migrationAPI.UpdateStatus)
		mux.HandleFunc("POST /api/v1/compute-pools/{poolID}/migrations/{migrationID}/prepare-cutover", migrationAPI.PrepareCutover)
	}

	resourceAPI := NewResourceAPI(store, bindings)
	mux.HandleFunc("PUT /api/v1/compute-pools/{resourceID}", resourceAPI.UpsertComputePool)
	mux.HandleFunc("GET /api/v1/compute-pools/{resourceID}", resourceAPI.GetComputePool)
	mux.HandleFunc("PUT /api/v1/workloads/{resourceID}", resourceAPI.UpsertWorkload)
	mux.HandleFunc("GET /api/v1/workloads/{resourceID}", resourceAPI.GetWorkload)

	stateAPI := NewStateAPI(store)
	mux.HandleFunc("GET /api/v1/internal/clusters/{clusterID}/state/{kind}/{resourceID}", stateAPI.Get)

	desiredAPI := NewDesiredAPI(store)
	mux.HandleFunc("PUT /api/v1/internal/clusters/{clusterID}/desired/{kind}/{resourceID}", desiredAPI.Upsert)
	mux.HandleFunc("DELETE /api/v1/internal/clusters/{clusterID}/desired/{kind}/{resourceID}", desiredAPI.Delete)
	return mux
}
