package domain

import "testing"

func TestPlacementMigrationRequestContract(t *testing.T) {
	m := PlacementMigration{
		Metadata:        Metadata{ID: "migration-1"},
		PoolID:          "pool-1",
		SourceClusterID: "cluster-a",
		TargetClusterID: "cluster-b",
		Phase:           PlacementMigrationRequested,
	}
	if !m.ValidateRequest() {
		t.Fatal("valid explicit cross-cluster migration request was rejected")
	}
	m.TargetClusterID = m.SourceClusterID
	if m.ValidateRequest() {
		t.Fatal("same-cluster update must not be modeled as a placement migration")
	}
}

func TestPlacementMigrationTerminalPhases(t *testing.T) {
	if !PlacementMigrationSucceeded.Terminal() || !PlacementMigrationFailed.Terminal() {
		t.Fatal("succeeded and failed must be terminal")
	}
	for _, phase := range []PlacementMigrationPhase{
		PlacementMigrationRequested,
		PlacementMigrationProjecting,
		PlacementMigrationReadyToCutover,
		PlacementMigrationCutover,
		PlacementMigrationRetiring,
	} {
		if phase.Terminal() {
			t.Fatalf("%s must not be terminal", phase)
		}
	}
}
