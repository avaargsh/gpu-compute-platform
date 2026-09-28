package integration

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/agent"
	"github.com/avaargsh/gpu-compute-platform/internal/agent/httpclient"
	"github.com/avaargsh/gpu-compute-platform/internal/cluster"
	"github.com/avaargsh/gpu-compute-platform/internal/platform/httpapi"
	"github.com/avaargsh/gpu-compute-platform/internal/store/agentstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestGoldenPathDesiredToObserved(t *testing.T) {
	ctx := context.Background()
	store := agentstore.NewMemory()
	store.SetDesired("cluster-a", []agent.DesiredResource{
		{
			Kind:       "ComputePool",
			ID:         "pool-h100",
			Generation: 1,
			Spec: map[string]any{
				"projectID": "project-1",
				"namespace": "project-1",
				"accelerators": []any{
					map[string]any{"class": "h100-80g", "quota": float64(8)},
				},
			},
		},
		{
			Kind:       "Workload",
			ID:         "train-1",
			Generation: 1,
			Spec: map[string]any{
				"projectID": "project-1",
				"poolID":    "pool-h100",
				"namespace": "project-1",
				"image":     "example/train:latest",
				"accelerator": map[string]any{
					"class": "h100-80g",
					"quota": float64(2),
				},
			},
		},
	})

	server := httptest.NewServer(httpapi.NewRouterWithAgentStore(store))
	defer server.Close()

	coreClient := kubefake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "workloads"}: "WorkloadList",
		},
	)
	runtime, err := cluster.NewRuntime(&cluster.Clients{Core: coreClient, Dynamic: dynamicClient})
	if err != nil {
		t.Fatal(err)
	}

	control := httpclient.New(server.URL, server.Client())
	runner := agent.NewRunner("cluster-a", control, runtime)
	if err := runner.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	observed := store.Observations("cluster-a")
	if len(observed) != 2 {
		t.Fatalf("observations=%d, want 2: %#v", len(observed), observed)
	}
	if observed[0].ID != "pool-h100" || observed[0].ObservedGeneration != 1 {
		t.Fatalf("unexpected pool observation: %#v", observed[0])
	}
	if observed[1].ID != "train-1" || observed[1].ObservedGeneration != 1 {
		t.Fatalf("unexpected workload observation: %#v", observed[1])
	}

	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-train-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("job was not projected: %v", err)
	}
	workloadObject := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "Workload",
		"metadata": map[string]any{
			"name":      "job-train-1-workload",
			"namespace": "project-1",
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "QuotaReserved", "status": "True"},
				map[string]any{"type": "Admitted", "status": "True"},
			},
		},
	}}
	workloadObject.SetOwnerReferences([]metav1.OwnerReference{{Kind: "Job", UID: job.UID}})
	workloadGVR := schema.GroupVersionResource{Group: "kueue.x-k8s.io", Version: "v1beta1", Resource: "workloads"}
	if _, err := dynamicClient.Resource(workloadGVR).Namespace("project-1").Create(ctx, workloadObject, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	_, err = coreClient.CoreV1().Pods("project-1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "job-train-1-pod",
			Labels: map[string]string{"ai.compute/workload": "job-train-1"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := runner.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	observed = store.Observations("cluster-a")
	var workload agent.Observation
	for _, item := range observed {
		if item.ID == "train-1" {
			workload = item
			break
		}
	}
	wantTrue := map[string]bool{"Ready": false, "QuotaReserved": false, "Admitted": false, "PodsReady": false}
	for _, condition := range workload.Conditions {
		if _, ok := wantTrue[condition.Type]; ok && condition.Status == "True" {
			wantTrue[condition.Type] = true
		}
	}
	for conditionType, found := range wantTrue {
		if !found {
			t.Fatalf("workload condition %s did not converge: %#v", conditionType, workload)
		}
	}
	if len(workload.EvidenceRefs) < 2 {
		t.Fatalf("expected job and kueue workload evidence: %#v", workload)
	}
}
