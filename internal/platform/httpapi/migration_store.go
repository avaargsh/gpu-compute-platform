package httpapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

type MigrationStore interface {
	CreatePlacementMigration(context.Context, domain.PlacementMigration) (domain.PlacementMigration, bool, error)
	GetPlacementMigration(context.Context, domain.ID, domain.ID) (domain.PlacementMigration, error)
	UpdatePlacementMigration(context.Context, domain.ID, domain.ID, domain.PlacementMigrationPhase, []domain.Condition, []string) (domain.PlacementMigration, error)
	CutoverPlacementMigration(context.Context, domain.ID, domain.ID) (domain.PlacementMigration, error)
}

func (r *MemoryPlacementResolver) CreatePlacementMigration(_ context.Context, in domain.PlacementMigration) (domain.PlacementMigration, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.pools[in.PoolID]
	if !ok {
		return domain.PlacementMigration{}, false, fmt.Errorf("cluster binding not found: %s", in.PoolID)
	}
	if current.ClusterID != in.SourceClusterID {
		return domain.PlacementMigration{}, false, agentstore.ErrPlacementSourceMismatch
	}
	if !in.ValidateRequest() || in.Metadata.Generation <= 0 {
		return domain.PlacementMigration{}, false, fmt.Errorf("invalid placement migration")
	}

	if r.migrations[in.PoolID] == nil {
		r.migrations[in.PoolID] = make(map[domain.ID]domain.PlacementMigration)
	}
	if existing, ok := r.migrations[in.PoolID][in.Metadata.ID]; ok {
		if sameMigrationIntent(existing, in) {
			return existing, false, nil
		}
		return domain.PlacementMigration{}, false, agentstore.ErrPlacementMigrationConflict
	}

	now := time.Now().UTC()
	in.Phase = domain.PlacementMigrationRequested
	in.Metadata.CreatedAt = now
	in.Metadata.UpdatedAt = now
	r.migrations[in.PoolID][in.Metadata.ID] = in
	return in, true, nil
}

func (r *MemoryPlacementResolver) GetPlacementMigration(_ context.Context, poolID, migrationID domain.ID) (domain.PlacementMigration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.migrations[poolID]
	if items == nil {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	out, ok := items[migrationID]
	if !ok {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	return out, nil
}

func sameMigrationIntent(a, b domain.PlacementMigration) bool {
	return a.Metadata.ID == b.Metadata.ID &&
		a.Metadata.Generation == b.Metadata.Generation &&
		a.PoolID == b.PoolID &&
		a.SourceClusterID == b.SourceClusterID &&
		a.TargetClusterID == b.TargetClusterID
}

func isMigrationConflict(err error) bool {
	return errors.Is(err, agentstore.ErrPlacementSourceMismatch) ||
		errors.Is(err, agentstore.ErrPlacementMigrationConflict)
}

func (r *MemoryPlacementResolver) UpdatePlacementMigration(_ context.Context, poolID, migrationID domain.ID, phase domain.PlacementMigrationPhase, conditions []domain.Condition, evidenceRefs []string) (domain.PlacementMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.migrations[poolID]
	if items == nil {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	current, ok := items[migrationID]
	if !ok {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	if !current.Phase.CanTransitionTo(phase) {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationTransition
	}
	current.Phase = phase
	current.Conditions = conditions
	current.EvidenceRefs = evidenceRefs
	current.Metadata.UpdatedAt = time.Now().UTC()
	items[migrationID] = current
	return current, nil
}

func (r *MemoryPlacementResolver) CutoverPlacementMigration(_ context.Context, poolID, migrationID domain.ID) (domain.PlacementMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	items := r.migrations[poolID]
	if items == nil {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	migration, ok := items[migrationID]
	if !ok {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationNotFound
	}
	if migration.Phase != domain.PlacementMigrationReadyToCutover {
		return domain.PlacementMigration{}, agentstore.ErrPlacementMigrationTransition
	}
	current, ok := r.pools[poolID]
	if !ok || current.ClusterID != migration.SourceClusterID {
		return domain.PlacementMigration{}, agentstore.ErrPlacementSourceMismatch
	}

	current.ClusterID = migration.TargetClusterID
	r.pools[poolID] = current
	migration.Phase = domain.PlacementMigrationCutover
	migration.Metadata.UpdatedAt = time.Now().UTC()
	items[migrationID] = migration
	return migration, nil
}
