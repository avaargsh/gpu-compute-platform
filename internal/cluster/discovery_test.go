package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestDiscoverFindsKueueAndAccelerators(t *testing.T) {
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{
		clusterQueueGVR: "ClusterQueueList",
	}
	clusterQueue := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "ClusterQueue",
		"metadata":   map[string]any{"name": "existing"},
	}}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		listKinds,
		clusterQueue,
	)
	coreClient := kubefake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   "gpu-1",
			Labels: map[string]string{"ai.compute/accelerator-class": "h100-80g"},
		}},
	)

	capabilities, err := (&Clients{Core: coreClient, Dynamic: dynamicClient}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Kueue {
		t.Fatal("expected kueue capability")
	}
	if len(capabilities.Accelerators) != 1 || capabilities.Accelerators[0] != "h100-80g" {
		t.Fatalf("unexpected accelerators: %#v", capabilities.Accelerators)
	}
}
