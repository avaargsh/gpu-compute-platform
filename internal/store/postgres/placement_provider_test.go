package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPostgresProviderBindingIsImmutable(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()

	if err := store.UpsertClusterBinding(ctx, domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 1},
		PoolID:   "pool-provider-fence",
		ClusterID: "cluster-a",
		Provider:  "kueue",
	}); err != nil {
		t.Fatal(err)
	}

	err := store.UpsertClusterBinding(ctx, domain.ClusterBinding{
		Metadata: domain.Metadata{Generation: 2},
		PoolID:   "pool-provider-fence",
		ClusterID: "cluster-a",
		Provider:  "volcano",
	})
	if !errors.Is(err, agentstore.ErrProviderBindingImmutable) {
		t.Fatalf("error=%v, want immutable provider binding", err)
	}

	placement, err := store.ResolvePool(ctx, "pool-provider-fence")
	if err != nil {
		t.Fatal(err)
	}
	if placement.ClusterID != "cluster-a" || placement.Provider != "kueue" {
		t.Fatalf("placement=%#v, want cluster-a/kueue", placement)
	}
}
