package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
)

func TestDesiredAPIWritesAgentPullState(t *testing.T) {
	store := agentstore.NewMemory()
	router := NewRouterWithAgentStore(store)

	body := `{"generation":7,"spec":{"projectID":"project-1","poolID":"pool-1","namespace":"project-1","image":"example/train:latest"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/cluster-a/desired/Workload/train-1", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upsert status=%d, want 204: %s", rec.Code, rec.Body.String())
	}

	items, err := store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "Workload" || items[0].ID != "train-1" || items[0].Generation != 7 {
		t.Fatalf("unexpected desired state: %#v", items)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/clusters/cluster-a/desired/Workload/train-1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d, want 204: %s", rec.Code, rec.Body.String())
	}
	items, err = store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("desired state not deleted: %#v", items)
	}
}
