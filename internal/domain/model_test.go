package domain

import "testing"

func TestObservedGenerationCanLagDesiredGeneration(t *testing.T) {
	pool := ComputePool{
		Metadata: Metadata{ID: "pool-1", Generation: 2},
		Status:   ResourceStatus{ObservedGeneration: 1},
	}

	if pool.Status.ObservedGeneration >= pool.Metadata.Generation {
		t.Fatalf("expected observed generation to lag desired generation")
	}
}

func TestProjectBindingKeepsProjectIndependentFromCluster(t *testing.T) {
	binding := ProjectBinding{
		ProjectID: "project-1",
		ClusterID: "cluster-a",
		Namespace: "project-1",
	}

	if binding.ProjectID == "" || binding.ClusterID == "" || binding.Namespace == "" {
		t.Fatalf("project binding must explicitly map project to cluster namespace")
	}
}
