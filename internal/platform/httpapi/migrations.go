package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type MigrationAPI struct {
	store MigrationStore
}

func NewMigrationAPI(store MigrationStore) *MigrationAPI {
	return &MigrationAPI{store: store}
}

func (a *MigrationAPI) Create(w http.ResponseWriter, r *http.Request) {
	poolID := domain.ID(r.PathValue("poolID"))
	var in domain.PlacementMigration
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid placement migration", http.StatusBadRequest)
		return
	}
	if poolID == "" || in.Metadata.ID == "" || in.Metadata.Generation <= 0 ||
		in.PoolID == "" || in.SourceClusterID == "" || in.TargetClusterID == "" {
		http.Error(w, "migration id, positive generation, poolId, sourceClusterId and targetClusterId are required", http.StatusBadRequest)
		return
	}
	if in.PoolID != poolID {
		http.Error(w, "pool id must match path", http.StatusBadRequest)
		return
	}
	if in.SourceClusterID == in.TargetClusterID {
		http.Error(w, "source and target clusters must differ", http.StatusBadRequest)
		return
	}
	in.Phase = domain.PlacementMigrationRequested

	out, created, err := a.store.CreatePlacementMigration(r.Context(), in)
	if err != nil {
		if isMigrationConflict(err) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (a *MigrationAPI) Get(w http.ResponseWriter, r *http.Request) {
	poolID := domain.ID(r.PathValue("poolID"))
	migrationID := domain.ID(r.PathValue("migrationID"))
	if poolID == "" || migrationID == "" {
		http.Error(w, "pool id and migration id are required", http.StatusBadRequest)
		return
	}
	out, err := a.store.GetPlacementMigration(r.Context(), poolID, migrationID)
	if err != nil {
		if errors.Is(err, agentstore.ErrPlacementMigrationNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type migrationStatusUpdate struct {
	Phase        domain.PlacementMigrationPhase `json:"phase"`
	Conditions   []domain.Condition             `json:"conditions,omitempty"`
	EvidenceRefs []string                       `json:"evidenceRefs,omitempty"`
}

func (a *MigrationAPI) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	poolID := domain.ID(r.PathValue("poolID"))
	migrationID := domain.ID(r.PathValue("migrationID"))
	var in migrationStatusUpdate
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Phase == "" {
		http.Error(w, "valid migration phase is required", http.StatusBadRequest)
		return
	}
	out, err := a.store.UpdatePlacementMigration(r.Context(), poolID, migrationID, in.Phase, in.Conditions, in.EvidenceRefs)
	if err != nil {
		if errors.Is(err, agentstore.ErrPlacementMigrationNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, agentstore.ErrPlacementMigrationTransition) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
