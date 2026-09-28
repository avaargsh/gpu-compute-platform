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

	publicReq := httptest.NewRequest(http.MethodPut, "/api/v1/clusters/cluster-a/desired/Workload/train-1", strings.NewReader(`{"generation":1,"spec":{}}`))
	publicRec := httptest.NewRecorder()
	router.ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusNotFound {
		t.Fatalf("raw desired public route status=%d, want 404", publicRec.Code)
	}

	body := `{"generation":7,"spec":{"projectID":"project-1","poolID":"pool-1","namespace":"project-1","image":"example/train:latest"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/internal/clusters/cluster-a/desired/Workload/train-1", strings.NewReader(body))
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

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/internal/clusters/cluster-a/desired/Workload/train-1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("delete status=%d, want 202: %s", rec.Code, rec.Body.String())
	}
	items, err = store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].DeletionTimestamp == nil {
		t.Fatalf("delete must preserve desired state while marking deletion: %#v", items)
	}
	if len(items[0].Finalizers) != 1 || items[0].Finalizers[0] != agentstore.ProviderCleanupFinalizer {
		t.Fatalf("delete must attach provider cleanup finalizer: %#v", items[0].Finalizers)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/internal/clusters/cluster-a/desired/Workload/train-1", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("repeated delete status=%d, want 202: %s", rec.Code, rec.Body.String())
	}
	items, err = store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Finalizers) != 1 {
		t.Fatalf("repeated delete must be idempotent: %#v", items)
	}

	if err := store.FinalizeDesired(context.Background(), "cluster-a", "Workload", "train-1", 7); err != nil {
		t.Fatal(err)
	}
	items, err = store.Desired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("finalize must hard-delete desired state: %#v", items)
	}
}
