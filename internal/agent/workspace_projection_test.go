package agent

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestDecodeDesiredWorkloadWorkspaceIntoProvider(t *testing.T) {
	var projection provider.WorkloadProjection
	err := decodeSpec(map[string]any{
		"image": "example/train:v1",
		"workspace": map[string]any{"claimName": "project-data"},
	}, &projection)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Workspace == nil || projection.Workspace.ClaimName != "project-data" {
		t.Fatalf("workspace not carried by Agent: %+v", projection.Workspace)
	}
}
