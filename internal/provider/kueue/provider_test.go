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

func (f *fakeClient) ApplyResourceClaim(_ context.Context, v Job) error {
	f.order = append(f.order, "claim:"+v.DRA.ClaimName)
	return nil
}

func (f *fakeClient) ApplyJob(_ context.Context, v Job) error {
	f.order = append(f.order, "job:"+v.Name)
	return nil
}

func (f *fakeClient) ObserveJob(_ context.Context, _, _ string) (JobObservation, error) {
	return JobObservation{}, nil
}
func (f *fakeClient) DeleteJob(_ context.Context, namespace, name string) (bool, error) {
	f.order = append(f.order, "delete-job:"+namespace+"/"+name)
	return true, nil
}
func (f *fakeClient) DeleteResourceClaim(_ context.Context, namespace, name string) (bool, error) {
	f.order = append(f.order, "delete-claim:"+namespace+"/"+name)
	return true, nil
}
func (f *fakeClient) DeleteResourceFlavor(_ context.Context, name string) (bool, error) {
	f.order = append(f.order, "delete-flavor:"+name)
	return true, nil
}
func (f *fakeClient) DeleteClusterQueue(_ context.Context, name string) (bool, error) {
	f.order = append(f.order, "delete-cq:"+name)
	return true, nil
}
func (f *fakeClient) DeleteLocalQueue(_ context.Context, namespace, name string) (bool, error) {
	f.order = append(f.order, "delete-lq:"+namespace+"/"+name)
	return true, nil
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

func TestDeletePoolPreservesSharedResourceFlavor(t *testing.T) {
	client := &fakeClient{}
	p := NewProvider(client)

	got, err := p.DeletePool(context.Background(), baseprovider.PoolProjection{
		PoolID:     "pool-1",
		ProjectID:  "project-1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 7,
		Accelerators: []domain.AcceleratorRequest{{
			Class: "h100-80g", Quota: 8,
		}},
		AcceleratorBindings: []domain.AcceleratorBinding{{
			Class: "h100-80g", ResourceName: "nvidia.com/gpu", Flavor: "h100-80g",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Gone {
		t.Fatalf("pool-owned resources should be gone: %#v", got)
	}
	want := []string{"delete-lq:project-1/lq-pool-1", "delete-cq:cq-pool-1"}
	if len(client.order) != len(want) {
		t.Fatalf("pool deletion must not delete shared flavor: %#v", client.order)
	}
	for i := range want {
		if client.order[i] != want[i] {
			t.Fatalf("delete[%d]=%q, want %q", i, client.order[i], want[i])
		}
	}
}

func TestDeleteDRAWorkloadReleasesClaimAndPreservesAuditEvidence(t *testing.T) {
	client := &fakeClient{}
	p := NewProvider(client)

	got, err := p.DeleteWorkload(context.Background(), baseprovider.WorkloadProjection{
		WorkloadID: "train-1",
		ProjectID:  "project-1",
		PoolID:     "pool-1",
		ClusterID:  "cluster-a",
		Namespace:  "project-1",
		Generation: 4,
		Accelerator: domain.AcceleratorRequest{
			Class: "a100-mig-1g",
			Quota: 1,
		},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class:          "a100-mig-1g",
			AllocationMode: domain.AcceleratorAllocationDRA,
			ResourceName:   "nvidia.com/gpu",
			Flavor:         "a100-mig-1g",
			DRA: &domain.DRAAllocation{
				DeviceClassName: "gpu.nvidia.com",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Gone {
		t.Fatalf("DRA workload resources should be gone: %#v", got)
	}

	wantOrder := []string{
		"delete-job:project-1/job-train-1",
		"delete-claim:project-1/accelerator-train-1",
	}
	if len(client.order) != len(wantOrder) {
		t.Fatalf("unexpected DRA release order: %#v", client.order)
	}
	for i := range wantOrder {
		if client.order[i] != wantOrder[i] {
			t.Fatalf("release[%d]=%q, want %q", i, client.order[i], wantOrder[i])
		}
	}

	wantEvidence := []string{
		"k8s://cluster-a/namespaces/project-1/jobs/job-train-1",
		"k8s://cluster-a/namespaces/project-1/resourceclaims/accelerator-train-1",
	}
	if len(got.EvidenceRefs) != len(wantEvidence) {
		t.Fatalf("release evidence=%#v, want %#v", got.EvidenceRefs, wantEvidence)
	}
	for i := range wantEvidence {
		if got.EvidenceRefs[i] != wantEvidence[i] {
			t.Fatalf("evidence[%d]=%q, want %q", i, got.EvidenceRefs[i], wantEvidence[i])
		}
	}
}
