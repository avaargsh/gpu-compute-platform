package cluster

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestDiscoverFindsKueueDRAAndAccelerators(t *testing.T) {
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
		readyKueuePod("kueue-active", "v0.19.6"),
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kueue-stale",
				Namespace: "kueue-system",
				Labels:    map[string]string{"app.kubernetes.io/name": "kueue"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "manager",
					Image: "registry.k8s.io/kueue/kueue:v0.18.0",
				}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
	)
	fakeDiscovery, ok := coreClient.Discovery().(*discoveryfake.FakeDiscovery)
	if !ok {
		t.Fatalf("discovery client=%T, want *fake.FakeDiscovery", coreClient.Discovery())
	}
	fakeDiscovery.Resources = []*metav1.APIResourceList{{
		GroupVersion: "resource.k8s.io/v1",
		APIResources: []metav1.APIResource{{Name: "resourceclaims"}},
	}}

	capabilities, err := (&Clients{Core: coreClient, Dynamic: dynamicClient}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !capabilities.Kueue {
		t.Fatal("expected kueue compatibility capability")
	}
	if len(capabilities.Schedulers) != 1 ||
		capabilities.Schedulers[0].Name != "kueue" ||
		capabilities.Schedulers[0].Version != "v0.19.6" {
		t.Fatalf("unexpected scheduler facts: %#v", capabilities.Schedulers)
	}
	if !capabilities.DRAAPIAvailable || capabilities.DRAAPIVersion != "resource.k8s.io/v1" {
		t.Fatalf("unexpected DRA facts: available=%t apiVersion=%q", capabilities.DRAAPIAvailable, capabilities.DRAAPIVersion)
	}
	if len(capabilities.Accelerators) != 1 || capabilities.Accelerators[0] != "h100-80g" {
		t.Fatalf("unexpected accelerators: %#v", capabilities.Accelerators)
	}
}

func TestDiscoverKueueVersionIsEmptyWhenReadyVersionsAreAmbiguous(t *testing.T) {
	coreClient := kubefake.NewSimpleClientset(
		readyKueuePod("kueue-old", "v0.18.0"),
		readyKueuePod("kueue-new", "v0.19.6"),
	)

	got := (&Clients{Core: coreClient}).discoverKueueVersion(context.Background())
	if got != "" {
		t.Fatalf("discoverKueueVersion()=%q, want empty for ambiguous ready versions", got)
	}
}

func TestImageTagDoesNotInventVersionFromDigest(t *testing.T) {
	if got := imageTag("registry.example/kueue@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); got != "" {
		t.Fatalf("imageTag(digest)=%q, want empty", got)
	}
	if got := imageTag("registry.example/kueue/kueue:v0.19.6"); got != "v0.19.6" {
		t.Fatalf("imageTag(tag)=%q, want v0.19.6", got)
	}
}

func readyKueuePod(name, version string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "kueue-system",
			Labels:    map[string]string{"app.kubernetes.io/name": "kueue"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "manager",
				Image: "registry.k8s.io/kueue/kueue:" + version,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}
