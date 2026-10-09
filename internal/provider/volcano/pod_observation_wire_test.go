package volcano

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// Exercise the actual client-go HTTP transport, not just dynamic/fake actions.
// The server is an in-process fake API endpoint: this is NOT Kubernetes API
// server acceptance or evidence of Volcano scheduling.
func TestVolcanoPodsReadyWireListThenReadback(t *testing.T) {
	for _, tt := range []struct {
		name          string
		readbackUID   string
		wantStatus    string
		wantReason    string
	}{
		{
			name: "consistent Pod HTTP readback",
			readbackUID: "pod-uid-1",
			wantStatus: "True",
			wantReason: "VolcanoOwnedPodReady",
		},
		{
			name: "recreated Pod same name over HTTP",
			readbackUID: "recreated-pod-uid",
			wantStatus: "Unknown",
			wantReason: "VolcanoPodReadbackDrift",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			job, pod := jobAndPodFixture(t)
			wantSelector := labels.Set{
				"volcano.sh/job-name": job.GetName(),
			}.AsSelector().String()
			jobPath := fmt.Sprintf("/apis/batch.volcano.sh/v1alpha1/namespaces/%s/jobs/%s",
				job.GetNamespace(), job.GetName())
			listPath := fmt.Sprintf("/api/v1/namespaces/%s/pods", job.GetNamespace())
			podPath := listPath + "/" + pod.GetName()

			var mu sync.Mutex
			var jobGets, podLists, podGets, forbiddenWrites int
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					forbiddenWrites++
					http.Error(w, "unexpected mutation", http.StatusMethodNotAllowed)
					return
				}
				switch r.URL.Path {
				case jobPath:
					jobGets++
					_ = json.NewEncoder(w).Encode(job.Object)
				case listPath:
					podLists++
					if got := r.URL.Query().Get("labelSelector"); got != wantSelector {
						t.Errorf("Pod LIST selector %q differs from job %q", got, wantSelector)
						http.Error(w, "bad selector", http.StatusBadRequest)
						return
					}
					if len(r.URL.Query()) != 1 {
						t.Errorf("unexpected Pod LIST query: %v", r.URL.Query())
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"apiVersion": "v1", "kind": "PodList",
						"metadata": map[string]any{"resourceVersion": "31"},
						"items": []any{pod.Object},
					})
				case podPath:
					podGets++
					if r.URL.RawQuery != "" {
						t.Errorf("Pod GET unexpectedly has query: %s", r.URL.RawQuery)
					}
					fresh := pod.DeepCopy()
					fresh.SetUID(types.UID(tt.readbackUID))
					_ = json.NewEncoder(w).Encode(fresh.Object)
				default:
					t.Errorf("unexpected Kubernetes dynamic REST path %s", r.URL.Path)
					http.Error(w, "not found", http.StatusNotFound)
				}
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			client, err := dynamic.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewProvider(client)
			if err != nil {
				t.Fatal(err)
			}
			observation, err := provider.ReconcileWorkload(
				context.Background(), adoptionWorkloadProjection(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if observation.Phase != "Running" || len(observation.Conditions) != 2 ||
				observation.Conditions[0].Type != "Ready" ||
				observation.Conditions[0].Status != "False" ||
				observation.Conditions[1].Type != "PodsReady" ||
				observation.Conditions[1].Status != tt.wantStatus ||
				observation.Conditions[1].Reason != tt.wantReason {
				t.Fatalf("unexpected wire-observed state: %#v", observation)
			}
			mu.Lock()
			defer mu.Unlock()
			if jobGets != 2 || podLists != 1 || podGets != 1 || forbiddenWrites != 0 {
				t.Fatalf("wire calls: jobs GET=%d, pods LIST=%d, pods GET=%d, writes=%d",
					jobGets, podLists, podGets, forbiddenWrites)
			}
		})
	}
}

var _ = unstructured.Unstructured{}
