package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestWorkloadWorkspacePVCFlowsThroughAPIAndReadback(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()
	body := []byte(`{"metadata":{"id":"train-ws","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"example/train:v1","command":["python","train.py"],"accelerator":{"class":"h100-80g","quota":1},"workspace":{"claimName":"training-data"}}}`)
	req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/train-ws", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("workspace PUT status=%d, want 204", response.StatusCode)
	}
	desired, found, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "train-ws")
	if err != nil || !found {
		t.Fatalf("no workspace Desired: found=%v err=%v", found, err)
	}
	raw, err := json.Marshal(desired.Spec["workspace"])
	if err != nil {
		t.Fatal(err)
	}
	var mounted domain.WorkspaceMount
	if err := json.Unmarshal(raw, &mounted); err != nil || mounted.ClaimName != "training-data" {
		t.Fatalf("persisted workspace: %+v error=%v", mounted, err)
	}
	read, err := server.Client().Get(server.URL + "/api/v1/workloads/train-ws")
	if err != nil {
		t.Fatal(err)
	}
	defer read.Body.Close()
	if read.StatusCode != http.StatusOK {
		t.Fatalf("read status=%d", read.StatusCode)
	}
	var got workloadView
	if err := json.NewDecoder(read.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Workspace == nil || got.Spec.Workspace.ClaimName != "training-data" {
		t.Fatalf("workspace missing from GET projection: %+v", got.Spec.Workspace)
	}
}

func TestInvalidWorkspaceClaimNameFailsBeforeDesiredWrite(t *testing.T) {
	store := agentstore.NewMemory()
	server := httptest.NewServer(boundRouter(store, "cluster-a", "cluster-a"))
	defer server.Close()
	for _, claim := range []string{"", "../escape", "project-b/data", "UPPER_PVC"} {
		value, err := json.Marshal(claim)
		if err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"metadata":{"id":"invalid-ws","generation":1},"projectId":"project-1","poolId":"pool-h100","spec":{"image":"train:v1","accelerator":{"class":"h100-80g","quota":1},"workspace":{"claimName":` + string(value) + `}}}`)
		req, err := http.NewRequest(http.MethodPut, server.URL+"/api/v1/workloads/invalid-ws", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("claim %q returned %d, want 400", claim, response.StatusCode)
		}
	}
	_, found, err := store.GetDesired(context.Background(), "cluster-a", "Workload", "invalid-ws")
	if err != nil || found {
		t.Fatalf("bad PVC created Desired: found=%v err=%v", found, err)
	}
}
