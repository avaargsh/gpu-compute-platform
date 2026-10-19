package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func newCreateUIDFixture(t *testing.T) (*Store, JobCreateUIDBinding) {
	t.Helper()
	db := openContractDB(t)
	ctx := context.Background()
	managementSchema, err := os.ReadFile("../../platform/management/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(managementSchema)); err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UnixNano()
	cluster := fmt.Sprintf("uidcluster-%d", suffix)
	project := fmt.Sprintf("uidproject-%d", suffix)
	pool := fmt.Sprintf("uidpool-%d", suffix)
	workload := fmt.Sprintf("uidworkload-%d", suffix)
	binding := JobCreateUIDBinding{
		OperationID: fmt.Sprintf("%032x", uint64(suffix)),
		ClusterID: domain.ID(cluster), ProjectID: project, WorkloadID: domain.ID(workload),
		Generation: 1, Namespace: "research", JobName: "job-train",
		JobUID: "uid-from-apiserver", JobResourceVersion: "77",
		LeaseOwner: "trusted-agent",
	}
	store := New(db)
	if err := store.UpsertDesired(ctx, binding.ClusterID, agent.DesiredResource{
		Kind: "Workload", ID: binding.WorkloadID, Generation: 1,
		Spec: map[string]any{"projectID": project, "poolID": pool},
	}); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []struct {
		query string
		args []any
	}{
		{`INSERT INTO project_bindings (project_id,cluster_id,namespace) VALUES ($1,$2,$3)`,
			[]any{project, cluster, binding.Namespace}},
		{`INSERT INTO cluster_bindings (pool_id,cluster_id,provider) VALUES ($1,$2,'kueue')`,
			[]any{pool, cluster}},
		{`INSERT INTO management_operation_audit
 (request_id,request_digest,issuer,subject,project_id,cluster_id,kind,resource_id,action,
  expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
 VALUES ($1,repeat('a',64),'https://idp.example','alice',$2,$3,'Workload',$4,
         'create',0,1,repeat('0',64),repeat('b',64))`,
			[]any{binding.OperationID, project, cluster, workload}},
	} {
		if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM management_operation_audit WHERE project_id=$1`, project)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM project_bindings WHERE project_id=$1`, project)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM cluster_bindings WHERE pool_id=$1`, pool)
	})
	claimed, err := store.ClaimReconcileLease(ctx, binding.ClusterID, "Workload", binding.WorkloadID, binding.LeaseOwner, 120)
	if err != nil || !claimed {
		t.Fatalf("claim reconcile lease: claimed=%t err=%v", claimed, err)
	}
	return store, binding
}

func TestJobCreateUIDRequiresOriginalAuditAndLiveLease(t *testing.T) {
	store, original := newCreateUIDFixture(t)
	ctx := context.Background()
	if err := store.RecordJobCreateUID(ctx, original); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, original); err != nil {
		t.Fatalf("exact idempotent replay: %v", err)
	}
	got, found, err := store.GetJobCreateUID(ctx, original.OperationID, original.ClusterID, original.WorkloadID, original.Generation)
	if err != nil || !found || !equalJobBinding(got, original) {
		t.Fatalf("stored UID read: %#v found=%t err=%v", got, found, err)
	}
	for _, testcase := range []struct {
		name string
		change func(*JobCreateUIDBinding)
		want error
	}{
		{"replaced-uid", func(x *JobCreateUIDBinding) { x.JobUID = "replacement-uid" }, ErrCreateUIDConflict},
		{"changed-resource-version", func(x *JobCreateUIDBinding) { x.JobResourceVersion = "88" }, ErrCreateUIDConflict},
		{"changed-job-name", func(x *JobCreateUIDBinding) { x.JobName = "foreign-job" }, ErrCreateUIDConflict},
		{"different-project", func(x *JobCreateUIDBinding) { x.ProjectID = "foreign" }, ErrCreateUIDUnproven},
		{"uncommitted-operation", func(x *JobCreateUIDBinding) { x.OperationID = strings.Repeat("f", 32) }, ErrCreateUIDUnproven},
		{"new-generation", func(x *JobCreateUIDBinding) { x.Generation = 2 }, ErrCreateUIDUnproven},
		{"wrong-namespace", func(x *JobCreateUIDBinding) { x.Namespace = "foreign" }, ErrCreateUIDUnproven},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			candidate := original
			testcase.change(&candidate)
			if err := store.RecordJobCreateUID(ctx, candidate); !errors.Is(err, testcase.want) {
				t.Fatalf("got %v want %v", err, testcase.want)
			}
		})
	}
	if _, found, err := store.GetJobCreateUID(ctx, original.OperationID, original.ClusterID, "other-workload", 1); err != nil || found {
		t.Fatalf("cross-resource binding escaped scope: found=%t err=%v", found, err)
	}
}

func TestJobCreateUIDLeaseTakeoverFencesOldOwner(t *testing.T) {
	store, input := newCreateUIDFixture(t)
	ctx := context.Background()
	_, err := store.db.ExecContext(ctx, `
UPDATE reconcile_leases SET lease_until=now()-interval '1 second'
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, input.ClusterID, input.WorkloadID)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimReconcileLease(ctx, input.ClusterID, "Workload", input.WorkloadID, "new-agent", 120)
	if err != nil || !claimed {
		t.Fatalf("lease takeover: claimed=%t err=%v", claimed, err)
	}
	if err := store.RecordJobCreateUID(ctx, input); !errors.Is(err, agentstore.ErrLeaseLost) {
		t.Fatalf("stale agent cannot publish UID: %v", err)
	}
	input.LeaseOwner = "new-agent"
	if err := store.MarkDesiredDeleting(ctx, input.ClusterID, "Workload", input.WorkloadID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, input); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("deleting Desired must reject UID: %v", err)
	}
}

func TestJobCreateUIDNoUnsignedLegacyOrMalformedOperation(t *testing.T) {
	store, original := newCreateUIDFixture(t)
	ctx := context.Background()
	invalid := original
	invalid.OperationID = "not-32-lowercase-hex"
	if err := store.RecordJobCreateUID(ctx, invalid); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("malformed operation: %v", err)
	}
	invalid = original
	invalid.JobUID = ""
	if err := store.RecordJobCreateUID(ctx, invalid); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("missing UID: %v", err)
	}
	_, err := store.db.ExecContext(ctx, `
UPDATE management_operation_audit SET request_digest=repeat('0',64) WHERE request_id=$1
`, original.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, original); !errors.Is(err, ErrCreateUIDUnproven) {
		t.Fatalf("legacy zero fingerprint must not bind: %v", err)
	}
}
