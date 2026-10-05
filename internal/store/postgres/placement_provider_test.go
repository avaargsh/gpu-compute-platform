package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPostgresClusterBindingKeepsProviderIdentityImmutable(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()

	initial := domain.ClusterBinding{
		Metadata:  domain.Metadata{Generation: 1},
		PoolID:    "pool-provider-identity",
		ClusterID: "cluster-a",
		Provider:  "kueue",
	}
	if err := store.UpsertClusterBinding(ctx, initial); err != nil {
		t.Fatal(err)
	}

	replay := initial
	replay.Metadata.Generation = 2
	if err := store.UpsertClusterBinding(ctx, replay); err != nil {
		t.Fatalf("same provider update failed: %v", err)
	}

	changed := replay
	changed.Metadata.Generation = 3
	changed.Provider = "volcano"
	if err := store.UpsertClusterBinding(ctx, changed); !errors.Is(err, agentstore.ErrProviderMigrationRequired) {
		t.Fatalf("provider change err=%v, want ErrProviderMigrationRequired", err)
	}

	got, err := store.ResolvePool(ctx, initial.PoolID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClusterID != initial.ClusterID || got.Provider != initial.Provider {
		t.Fatalf("provider identity changed after rejected rebind: %#v", got)
	}
}
