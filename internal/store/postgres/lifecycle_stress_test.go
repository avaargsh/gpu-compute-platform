package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

const lifecycleStressIterations = 20

func TestPostgresLifecycleRaceStressPreservesDeletionFence(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-lifecycle-stress")
	deletedAt := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)

	for i := 0; i < lifecycleStressIterations; i++ {
		resourceID := domain.ID(fmt.Sprintf("pool-delete-upsert-%02d", i))

		if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
			Kind:       "ComputePool",
			ID:         resourceID,
			Generation: 1,
			Spec: map[string]any{
				"quota": 1,
			},
		}); err != nil {
			t.Fatalf("iteration %d seed desired: %v", i, err)
		}

		start := make(chan struct{})
		results := make(chan error, 2)

		go func() {
			<-start
			results <- store.MarkDesiredDeleting(
				context.Background(),
				clusterID,
				"ComputePool",
				resourceID,
				deletedAt,
			)
		}()
		go func() {
			<-start
			results <- store.UpsertDesired(
				context.Background(),
				clusterID,
				agent.DesiredResource{
					Kind:       "ComputePool",
					ID:         resourceID,
					Generation: 2,
					Spec: map[string]any{
						"quota": 2,
					},
				},
			)
		}()

		close(start)
		for attempt := 0; attempt < 2; attempt++ {
			if err := <-results; err != nil {
				t.Fatalf("iteration %d concurrent lifecycle mutation: %v", i, err)
			}
		}

		got, found, err := store.GetDesired(
			ctx,
			clusterID,
			"ComputePool",
			resourceID,
		)
		if err != nil {
			t.Fatalf("iteration %d get desired: %v", i, err)
		}
		if !found {
			t.Fatalf("iteration %d desired disappeared", i)
		}
		if got.Generation != 2 {
			t.Fatalf(
				"iteration %d generation=%d, want 2",
				i,
				got.Generation,
			)
		}
		if got.DeletionTimestamp == nil ||
			!got.DeletionTimestamp.Equal(deletedAt) {
			t.Fatalf(
				"iteration %d deletion fence lost: %#v",
				i,
				got.DeletionTimestamp,
			)
		}

		hasCleanupFinalizer := false
		for _, finalizer := range got.Finalizers {
			if finalizer == agentstore.ProviderCleanupFinalizer {
				hasCleanupFinalizer = true
				break
			}
		}
		if !hasCleanupFinalizer {
			t.Fatalf(
				"iteration %d provider cleanup finalizer lost: %#v",
				i,
				got.Finalizers,
			)
		}
	}
}

func TestPostgresLeaseClaimStressHasSingleLiveOwner(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	clusterID := domain.ID("cluster-lease-stress")
	const contenders = 8

	type claimResult struct {
		owner   string
		claimed bool
		err     error
	}

	for i := 0; i < lifecycleStressIterations; i++ {
		resourceID := domain.ID(fmt.Sprintf("workload-lease-%02d", i))
		if err := store.UpsertDesired(ctx, clusterID, agent.DesiredResource{
			Kind:       "Workload",
			ID:         resourceID,
			Generation: 1,
			Spec:       map[string]any{},
		}); err != nil {
			t.Fatalf("iteration %d seed desired: %v", i, err)
		}

		start := make(chan struct{})
		results := make(chan claimResult, contenders)

		for contender := 0; contender < contenders; contender++ {
			owner := fmt.Sprintf("agent-%02d", contender)
			go func(owner string) {
				<-start
				claimed, err := store.ClaimReconcileLease(
					context.Background(),
					clusterID,
					"Workload",
					resourceID,
					owner,
					120,
				)
				results <- claimResult{
					owner:   owner,
					claimed: claimed,
					err:     err,
				}
			}(owner)
		}

		close(start)

		claims := 0
		winner := ""
		for contender := 0; contender < contenders; contender++ {
			result := <-results
			if result.err != nil {
				t.Fatalf(
					"iteration %d owner %s claim failed: %v",
					i,
					result.owner,
					result.err,
				)
			}
			if result.claimed {
				claims++
				winner = result.owner
			}
		}

		if claims != 1 {
			t.Fatalf(
				"iteration %d live owners=%d, want exactly 1",
				i,
				claims,
			)
		}

		for contender := 0; contender < contenders; contender++ {
			owner := fmt.Sprintf("agent-%02d", contender)
			if owner == winner {
				continue
			}
			claimed, err := store.ClaimReconcileLease(
				ctx,
				clusterID,
				"Workload",
				resourceID,
				owner,
				120,
			)
			if err != nil {
				t.Fatalf(
					"iteration %d verify loser %s: %v",
					i,
					owner,
					err,
				)
			}
			if claimed {
				t.Fatalf(
					"iteration %d loser %s acquired live lease held by %s",
					i,
					owner,
					winner,
				)
			}
		}
	}
}
