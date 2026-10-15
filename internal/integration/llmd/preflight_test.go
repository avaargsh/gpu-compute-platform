package llmd

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func testPool() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": groupVersion,
		"kind":       "InferencePool",
		"metadata": map[string]any{
			"name": "model-a", "namespace": "serving", "uid": "pool-uid",
			"resourceVersion": "42", "generation": int64(3),
		},
		"spec": map[string]any{
			"selector":    map[string]any{"matchLabels": map[string]any{"app": "model-a"}},
			"targetPorts": []any{map[string]any{"number": int64(8000)}},
		},
	}}
}

func testClients(pool *unstructured.Unstructured) (*discoveryfake.FakeDiscovery, *dynamicfake.FakeDynamicClient) {
	core := kubefake.NewSimpleClientset()
	discover := core.Discovery().(*discoveryfake.FakeDiscovery)
	discover.Resources = []*metav1.APIResourceList{{
		GroupVersion: groupVersion,
		APIResources: []metav1.APIResource{{Name: "inferencepools", Kind: "InferencePool", Namespaced: true}},
	}}
	objects := []runtime.Object{}
	if pool != nil {
		objects = append(objects, pool)
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{poolGVR: "InferencePoolList"},
		objects...,
	)
	return discover, dyn
}

func TestReadOnlyPreflightObservesPoolWithoutPromotingReady(t *testing.T) {
	discover, dyn := testClients(testPool())
	got, err := Probe(context.Background(), discover, dyn, "serving", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "OBSERVED_ONLY" || !got.APIServed || !got.ObjectObserved ||
		got.UID != "pool-uid" || got.ResourceVersion != "42" ||
		got.Generation != 3 || got.ServingReadyProven {
		t.Fatalf("unexpected preflight: %+v", got)
	}
	if len(dyn.Actions()) != 1 || !dyn.Actions()[0].Matches("get", "inferencepools") {
		t.Fatalf("preflight must issue one read-only Get: %#v", dyn.Actions())
	}
}

func TestPreflightFailsClosedWithoutV1API(t *testing.T) {
	discover, dyn := testClients(testPool())
	discover.Resources = []*metav1.APIResourceList{{
		GroupVersion: groupVersion,
		APIResources: []metav1.APIResource{{Name: "someotherresources", Namespaced: true}},
	}}
	got, err := Probe(context.Background(), discover, dyn, "serving", "model-a")
	if err != nil || got.Status != "BLOCKED" || got.APIServed || got.ServingReadyProven {
		t.Fatalf("missing API incorrectly accepted: result=%+v err=%v", got, err)
	}
	if len(dyn.Actions()) != 0 {
		t.Fatal("must not read an unsupported API")
	}
}

func TestPreflightFailsClosedWhenPoolAbsent(t *testing.T) {
	discover, dyn := testClients(nil)
	got, err := Probe(context.Background(), discover, dyn, "serving", "model-a")
	if err != nil || got.Status != "BLOCKED" || got.Reason != "InferencePoolNotFound" {
		t.Fatalf("missing pool: result=%+v err=%v", got, err)
	}
}

func TestPreflightRejectsUnprovenServerIdentity(t *testing.T) {
	cases := []struct {
		name   string
		change func(*unstructured.Unstructured)
	}{
		{"wrong version", func(p *unstructured.Unstructured) { p.SetAPIVersion("inference.networking.x-k8s.io/v1alpha2") }},
		{"wrong kind", func(p *unstructured.Unstructured) { p.SetKind("OtherResource") }},
		{"missing UID", func(p *unstructured.Unstructured) { p.SetUID("") }},
		{"missing RV", func(p *unstructured.Unstructured) { p.SetResourceVersion("") }},
		{"missing generation", func(p *unstructured.Unstructured) { p.SetGeneration(0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := testPool()
			tc.change(pool)
			discover, dyn := testClients(pool)
			got, err := Probe(context.Background(), discover, dyn, "serving", "model-a")
			if err != nil || got.Status != "BLOCKED" || got.ServingReadyProven {
				t.Fatalf("unproven pool accepted: result=%+v err=%v", got, err)
			}
		})
	}
}

func TestPreflightRejectsInvalidIdentityBeforeKubernetesRead(t *testing.T) {
	discover, dyn := testClients(testPool())
	got, err := Probe(context.Background(), discover, dyn, "../other", "model-a")
	if err == nil || got.Reason != "InvalidResourceIdentity" || len(dyn.Actions()) != 0 {
		t.Fatalf("invalid identity: result=%+v err=%v", got, err)
	}
}

func TestPreflightRejectsRBACFailure(t *testing.T) {
	discover, dyn := testClients(testPool())
	dyn.PrependReactor("get", "inferencepools", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	got, err := Probe(context.Background(), discover, dyn, "serving", "model-a")
	if err == nil || got.Status != "BLOCKED" || got.Reason != "InferencePoolReadFailed" {
		t.Fatalf("read failed open: result=%+v err=%v", got, err)
	}
}
