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
	"k8s.io/apimachinery/pkg/runtime"
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
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
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

	if _, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-train-1", metav1.GetOptions{}); err != nil {
		t.Fatalf("job was not projected: %v", err)
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
	if len(workload.Conditions) != 1 {
		t.Fatalf("workload conditions missing: %#v", workload)
	}
	if workload.Conditions[0].Status != "True" || workload.Conditions[0].Reason != "PodsReady" {
		t.Fatalf("workload did not converge to ready: %#v", workload)
	}
}
