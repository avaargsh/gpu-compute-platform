package httpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
)

func TestPullDesired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("clusterId") != "cluster-a" {
			t.Fatalf("unexpected cluster id: %s", r.URL.Query().Get("clusterId"))
		}
		_ = json.NewEncoder(w).Encode([]agent.DesiredResource{
			{Kind: "ComputePool", ID: "pool-1", Generation: 2},
		})
	}))
	defer server.Close()

	client := New(server.URL, server.Client())
	got, err := client.PullDesired(context.Background(), "cluster-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "pool-1" {
		t.Fatalf("unexpected desired resources: %#v", got)
	}
}
