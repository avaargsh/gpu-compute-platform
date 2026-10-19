package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func TestDesiredProjectsOriginalManagementOperationWithoutSpecInjection(t *testing.T) {
	db := openContractDB(t)
	ctx := context.Background()
	schema, err := os.ReadFile("../../platform/management/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	id := time.Now().UnixNano()
	cluster := fmt.Sprintf("opcluster-%d", id)
	project := fmt.Sprintf("opproject-%d", id)
	workload := fmt.Sprintf("opworkload-%d", id)
	originalID := fmt.Sprintf("%032x", uint64(id))
	otherID := fmt.Sprintf("%032x", uint64(id+1))
	store := New(db)
	if err := store.UpsertDesired(ctx, domain.ID(cluster), agent.DesiredResource{
		Kind: "Workload", ID: domain.ID(workload), Generation: 1,
		Spec: map[string]any{
			"projectID": project, "poolID": "pool-a", "operationId": "user-forged",
		},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			"DELETE FROM management_operation_audit WHERE cluster_id=$1 AND resource_id=$2", cluster, workload)
	})
	get := func() []agent.DesiredResource {
		t.Helper()
		items, err := store.Desired(ctx, domain.ID(cluster))
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected one Workload, got %d", len(items))
		}
		return items
	}
	if got := get()[0].OperationID; got != "" {
		t.Fatalf("unaudited user spec forged operation ID %q", got)
	}
	insertAudit := func(id, digest string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
INSERT INTO management_operation_audit
(request_id,request_digest,issuer,subject,project_id,cluster_id,kind,resource_id,action,
 expected_generation,resulting_generation,prior_spec_sha256,resulting_spec_sha256)
VALUES ($1,$2,'https://idp.example','alice',$3,$4,'Workload',$5,'create',0,1,
 repeat('0',64),repeat('a',64))`, id, digest, project, cluster, workload)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertAudit(originalID, fmt.Sprintf("%064d", 0))
	if got := get()[0].OperationID; got != "" {
		t.Fatalf("legacy zero-fingerprint audit was promoted: %q", got)
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE management_operation_audit SET request_digest=repeat('a',64) WHERE request_id=$1", originalID,
	); err != nil {
		t.Fatal(err)
	}
	if got := get()[0].OperationID; got != originalID {
		t.Fatalf("original Management CREATE ID not projected: %q", got)
	}
	insertAudit(otherID, fmt.Sprintf("%064x", 1))
	if _, err := store.Desired(ctx, domain.ID(cluster)); err == nil {
		t.Fatal("ambiguous dual CREATE audits must fail closed, not choose first")
	}
}
