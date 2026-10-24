package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

func durableAttempt(b JobCreateUIDBinding) verifiedops.JobCreateAttempt {
	return verifiedops.JobCreateAttempt{
		OperationID: b.OperationID,
		ClusterID:   b.ClusterID,
		ProjectID:   b.ProjectID,
		WorkloadID:  b.WorkloadID,
		Generation:  b.Generation,
		Namespace:   b.Namespace,
		JobName:     b.JobName,
		LeaseOwner:  b.LeaseOwner,
	}
}

func TestDurableCreateAttemptCrashAndReplayRemainUnknown(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	if state, found, err := store.GetJobCreateAttemptState(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation); err != nil || found || state != "" {
		t.Fatalf("unexpected historical PREPARED: %q %v %v", state, found, err)
	}
	if err := store.RecordJobCreateUID(ctx, binding); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("must not bind UID before durable PREPARED: %v", err)
	}
	prepared := durableAttempt(binding)
	if err := store.PrepareJobCreateAttempt(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	// Re-reading the durable row in a new store instance simulates a new
	// Agent / server process after an ACK loss or crash.
	restarted := New(store.db)
	if state, found, err := restarted.GetJobCreateAttemptState(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation); err != nil || !found || state != "UNKNOWN" {
		t.Fatalf("PREPARED must be UNKNOWN after restart: %q found %v err %v", state, found, err)
	}
	if err := restarted.PrepareJobCreateAttempt(ctx, prepared); !errors.Is(err, verifiedops.ErrCreateAttemptUnresolved) {
		t.Fatalf("same owner cannot repeat ambiguous CREATE: %v", err)
	}
	if err := restarted.RecordJobCreateUID(ctx, binding); err != nil {
		t.Fatalf("genuine CREATE ACK while lease held: %v", err)
	}
	if state, found, err := restarted.GetJobCreateAttemptState(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation); err != nil || !found || state != "ACK_BOUND" {
		t.Fatalf("ACK-bound row not recognized: %q found %v err %v", state, found, err)
	}
	if err := restarted.PrepareJobCreateAttempt(ctx, prepared); !errors.Is(err, verifiedops.ErrCreateAttemptUnresolved) {
		t.Fatalf("bound operation must not reissue CREATE: %v", err)
	}
	replacement := binding
	replacement.JobUID = "same-name-new-object-uid"
	if err := restarted.RecordJobCreateUID(ctx, replacement); !errors.Is(err, ErrCreateUIDConflict) {
		t.Fatalf("replacement UID incorrectly rebound: %v", err)
	}
}

func TestCreateAttemptLeaseTakeoverCannotReissueOrRebind(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
UPDATE reconcile_leases SET lease_until=now()-interval '1 second'
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, binding.ClusterID, binding.WorkloadID); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(ctx, binding.ClusterID, "Workload", binding.WorkloadID, "successor-agent", 120)
	if err != nil || !claimed {
		t.Fatalf("lease takeover failed: %v claimed=%v", err, claimed)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale owner can still prepare CREATE: %v", err)
	}
	if err := store.RecordJobCreateUID(ctx, binding); !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale owner can publish ACK: %v", err)
	}
	takeover := durableAttempt(binding)
	takeover.LeaseOwner = "successor-agent"
	if err := store.PrepareJobCreateAttempt(ctx, takeover); !errors.Is(err, verifiedops.ErrCreateAttemptUnresolved) {
		t.Fatalf("successor reissued unresolved CREATE: %v", err)
	}
	replacement := binding
	replacement.LeaseOwner = takeover.LeaseOwner
	if err := store.RecordJobCreateUID(ctx, replacement); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("successor promoted a GET response to original ACK: %v", err)
	}
	state, found, err := store.GetJobCreateAttemptState(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation)
	if err != nil || !found || state != "UNKNOWN" {
		t.Fatalf("lease takeover changed unresolved state: %q %v %v", state, found, err)
	}
}

func TestCreateAttemptScopeValidationAndDeletingState(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		change func(*verifiedops.JobCreateAttempt)
	}{
		{"wrong-project", func(in *verifiedops.JobCreateAttempt) { in.ProjectID = "foreign" }},
		{"missing-audit", func(in *verifiedops.JobCreateAttempt) { in.OperationID = "ffffffffffffffffffffffffffffffff" }},
		{"wrong-job-name", func(in *verifiedops.JobCreateAttempt) { in.JobName = "job-foreign" }},
		{"wrong-namespace", func(in *verifiedops.JobCreateAttempt) { in.Namespace = "foreign" }},
		{"wrong-generation", func(in *verifiedops.JobCreateAttempt) { in.Generation++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := durableAttempt(binding)
			tc.change(&candidate)
			if err := store.PrepareJobCreateAttempt(ctx, candidate); !errors.Is(err, ErrCreateUIDUnproven) {
				t.Fatalf("invalid scope accepted: %v", err)
			}
		})
	}
	if err := store.MarkDesiredDeleting(ctx, binding.ClusterID, "Workload", binding.WorkloadID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("deleting Desired allowed new CREATE intent: %v", err)
	}
}

func TestResolveJobCreateAttemptDistinguishesNotFoundUnknownBoundAndStale(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	resolve := func() verifiedops.JobCreateAttemptResolution {
		t.Helper()
		got, err := store.ResolveJobCreateAttempt(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation)
		if err != nil { t.Fatal(err) }
		return got
	}
	if got := resolve(); got.State != verifiedops.JobCreateAttemptNotFound || got.Binding != nil {
		t.Fatalf("missing durable attempt must be NOT_FOUND: %#v", got)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); err != nil { t.Fatal(err) }
	restarted := New(store.db)
	unknown, err := restarted.ResolveJobCreateAttempt(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation)
	if err != nil || unknown.State != verifiedops.JobCreateAttemptUnknown || unknown.Binding != nil {
		t.Fatalf("PREPARED without ACK must remain UNKNOWN: %#v err=%v", unknown, err)
	}
	if err := restarted.RecordJobCreateUID(ctx, binding); err != nil { t.Fatal(err) }
	bound, err := restarted.ResolveJobCreateAttempt(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation)
	if err != nil || bound.State != verifiedops.JobCreateAttemptACKBound || bound.Binding == nil ||
		bound.Binding.JobUID != binding.JobUID || bound.Binding.JobResourceVersion != binding.JobResourceVersion ||
		bound.Binding.OperationID != binding.OperationID || bound.Binding.Generation != binding.Generation {
		t.Fatalf("ACK-bound resolution lost exact provider identity: %#v err=%v", bound, err)
	}
	if _, err := restarted.db.ExecContext(ctx, `
	UPDATE desired_resources SET generation=generation+1
	WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
	`, binding.ClusterID, binding.WorkloadID); err != nil { t.Fatal(err) }
	stale, err := restarted.ResolveJobCreateAttempt(ctx, binding.OperationID, binding.ClusterID, binding.WorkloadID, binding.Generation)
	if err != nil || stale.State != verifiedops.JobCreateAttemptStale || stale.Binding != nil {
		t.Fatalf("old generation must not expose stale binding: %#v err=%v", stale, err)
	}
}
