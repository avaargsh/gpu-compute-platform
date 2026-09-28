package kueue

import (
	"context"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type fakeClient struct {
	order []string
}

func (f *fakeClient) ApplyResourceFlavor(_ context.Context, v ResourceFlavor) error {
	f.order = append(f.order, "flavor:"+v.Name)
	return nil
}

func (f *fakeClient) ApplyClusterQueue(_ context.Context, v ClusterQueue) error {
	f.order = append(f.order, "cq:"+v.Name)
	return nil
}

func (f *fakeClient) ApplyLocalQueue(_ context.Context, v LocalQueue) error {
	f.order = append(f.order, "lq:"+v.Name)
	return nil
}

func (f *fakeClient) ApplyJob(_ context.Context, v Job) error {
	f.order = append(f.order, "job:"+v.Name)
	return nil
}

func (f *fakeClient) ObserveJob(_ context.Context, _, _ string) (JobObservation, error) {
	return JobObservation{}, nil
}

func TestProviderAppliesPoolResourcesInDependencyOrder(t *testing.T) {
	client := &fakeClient{}
	p := NewProvider(client)

	got, err := p.ReconcilePool(context.Background(), baseprovider.PoolProjection{
		PoolID:              "pool-1",
		ProjectID:           "project-1",
		ClusterID:           "cluster-a",
		Namespace:           "project-1",
		Generation:          7,
		Accelerators:        []domain.AcceleratorRequest{{Class: "h100-80g", Quota: 8}},
		AcceleratorBindings: []domain.AcceleratorBinding{{Class: "h100-80g", ResourceName: "nvidia.com/gpu", Flavor: "h100-80g"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"flavor:h100-80g", "cq:cq-pool-1", "lq:lq-pool-1"}
	if len(client.order) != len(want) {
		t.Fatalf("unexpected apply order: %#v", client.order)
	}
	for i := range want {
		if client.order[i] != want[i] {
			t.Fatalf("apply[%d]=%q, want %q", i, client.order[i], want[i])
		}
	}
	if got.ObservedGeneration != 7 {
		t.Fatalf("observed generation=%d", got.ObservedGeneration)
	}
	if len(got.EvidenceRefs) != 2 {
		t.Fatalf("expected evidence refs, got %#v", got.EvidenceRefs)
	}
}
