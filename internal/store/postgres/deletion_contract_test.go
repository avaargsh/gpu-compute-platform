package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestPostgresMissingDesiredObservationCAS(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, generation := range []int64{6, 7, 8} {
		id := domain.ID([]string{"older", "matching", "future"}[i])
		if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: id, ObservedGeneration: generation}}); err != nil {
			t.Fatalf("report before desired must be supported: %v", err)
		}
		if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: id, Generation: 7}); err != nil {
			t.Fatal(err)
		}
		if _, found, err := store.GetObservation(ctx, "cluster-a", "Workload", id); err != nil || found != (generation == 7) {
			t.Fatalf("provisional report generation=%d found=%t err=%v", generation, found, err)
		}
	}

	// Hold the lifecycle identity before desired exists, like UpsertDesired.
	// Report must wait, then read the committed desired generation for its CAS.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := lockResourceTx(ctx, tx, "cluster-a", "Workload", "concurrent"); err != nil {
		t.Fatal(err)
	}
	reported := make(chan error, 1)
	go func() {
		reported <- store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "concurrent", ObservedGeneration: 6}})
	}()
	for {
		select {
		case err := <-reported:
			t.Fatalf("report bypassed missing-row lifecycle lock: %v", err)
		default:
		}
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event = 'advisory' AND query LIKE '%pg_advisory_xact_lock%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO desired_resources (cluster_id, kind, resource_id, generation, spec) VALUES ('cluster-a', 'Workload', 'concurrent', 7, '{}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-reported; err != nil {
		t.Fatalf("report did not recheck committed desired generation: %v", err)
	}
	if _, found, err := store.GetObservation(ctx, "cluster-a", "Workload", "concurrent"); err != nil || found {
		t.Fatalf("stale concurrent observation survived: found=%t err=%v", found, err)
	}
}

func TestPostgresFinalizeFailureRollsBackEvidenceAndRuntimeCleanup(t *testing.T) {
	db := openContractDB(t)
	store := New(db)
	ctx := context.Background()
	if err := store.UpsertDesired(ctx, "cluster-a", agent.DesiredResource{Kind: "Workload", ID: "rollback", Generation: 7}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDesiredDeleting(ctx, "cluster-a", "Workload", "rollback", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "rollback", 7); !errors.Is(err, agentstore.ErrDeletionEvidenceRequired) {
		t.Fatalf("missing evidence must block finalize: %v", err)
	}
	if err := store.Report(ctx, "cluster-a", []agent.Observation{{Kind: "Workload", ID: "rollback", ObservedGeneration: 7, Conditions: []domain.Condition{{Type: "Ready", Status: "False", Reason: "Deleted"}}, EvidenceRefs: []string{"provider://gone/rollback"}}}); err != nil {
		t.Fatal(err)
	}
	grant, err := store.ClaimReconcileLease(ctx, "cluster-a", "Workload", "rollback", "worker", 120)
	if err != nil || !grant.Claimed {
		t.Fatalf("claim: %#v %v", grant, err)
	}
	if _, err := db.ExecContext(ctx, `CREATE FUNCTION fail_desired_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected final delete failure'; END $$;
CREATE TRIGGER fail_desired_delete BEFORE DELETE ON desired_resources FOR EACH ROW EXECUTE FUNCTION fail_desired_delete();`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS fail_desired_delete ON desired_resources; DROP FUNCTION IF EXISTS fail_desired_delete();`)
	})
	if err := store.FinalizeDesired(ctx, "cluster-a", "Workload", "rollback", 7, grant.Owner, grant.Epoch); err == nil {
		t.Fatal("injected hard delete failure must fail finalization")
	}
	desired, found, err := store.GetDesired(ctx, "cluster-a", "Workload", "rollback")
	if err != nil || !found || len(desired.Finalizers) != 1 || desired.DeletionTimestamp == nil {
		t.Fatalf("finalizer rollback failed: %#v found=%t err=%v", desired, found, err)
	}
	if _, found, err := store.GetObservation(ctx, "cluster-a", "Workload", "rollback"); err != nil || !found {
		t.Fatalf("observation rollback failed: found=%t err=%v", found, err)
	}
	if _, found, err := store.GetDeletionTombstone(ctx, "cluster-a", "Workload", "rollback"); err != nil || found {
		t.Fatalf("uncommitted tombstone survived: found=%t err=%v", found, err)
	}
	var leases int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM reconcile_leases WHERE resource_id = 'rollback'`).Scan(&leases); err != nil || leases != 1 {
		t.Fatalf("lease rollback failed: count=%d err=%v", leases, err)
	}
}

func TestPostgresReversedReportBatchesDoNotDeadlock(t *testing.T) {
	store := New(openContractDB(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	observations := []agent.Observation{{Kind: "Workload", ID: "a", ObservedGeneration: 1}, {Kind: "Workload", ID: "b", ObservedGeneration: 1}}
	errors := make(chan error, 2)
	start := make(chan struct{})
	for _, batch := range [][]agent.Observation{observations, {observations[1], observations[0]}} {
		go func(batch []agent.Observation) {
			<-start
			for i := 0; i < 20; i++ {
				if err := store.Report(ctx, "cluster-a", batch); err != nil {
					errors <- err
					return
				}
			}
			errors <- nil
		}(batch)
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
}
