package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func (a *MigrationAPI) RetireSource(w http.ResponseWriter, r *http.Request) {
	poolID := domain.ID(r.PathValue("poolID"))
	migrationID := domain.ID(r.PathValue("migrationID"))
	migration, err := a.store.GetPlacementMigration(r.Context(), poolID, migrationID)
	if err != nil {
		if errors.Is(err, agentstore.ErrPlacementMigrationNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if migration.Phase != domain.PlacementMigrationRetiring {
		http.Error(w, agentstore.ErrPlacementMigrationTransition.Error(), http.StatusConflict)
		return
	}

	desired, found, err := a.resources.GetDesired(r.Context(), migration.SourceClusterID, "ComputePool", poolID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if found {
		if desired.DeletionTimestamp == nil {
			if err := a.resources.MarkDesiredDeleting(r.Context(), migration.SourceClusterID, "ComputePool", poolID, time.Now().UTC()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if _, observed, err := a.resources.GetObservation(r.Context(), migration.SourceClusterID, "ComputePool", poolID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if observed {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	evidence := append([]string(nil), migration.EvidenceRefs...)
	evidence = append(evidence, "control-plane://placement-migration/source-gone")
	out, err := a.store.UpdatePlacementMigration(
		r.Context(), poolID, migrationID, domain.PlacementMigrationSucceeded, migration.Conditions, evidence,
	)
	if err != nil {
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
