package domain

import "testing"

func TestPlacementMigrationTransitionsAreMonotonic(t *testing.T) {
	path := []PlacementMigrationPhase{
		PlacementMigrationRequested,
		PlacementMigrationProjecting,
		PlacementMigrationReadyToCutover,
		PlacementMigrationCutover,
		PlacementMigrationRetiring,
		PlacementMigrationSucceeded,
	}
	for i := 0; i < len(path)-1; i++ {
		if !path[i].CanTransitionTo(path[i+1]) {
			t.Fatalf("%s must transition to %s", path[i], path[i+1])
		}
		if path[i+1].CanTransitionTo(path[i]) {
			t.Fatalf("%s must not transition backwards to %s", path[i+1], path[i])
		}
	}
}

func TestPlacementMigrationFailureAndTerminalRules(t *testing.T) {
	for _, phase := range []PlacementMigrationPhase{
		PlacementMigrationRequested,
		PlacementMigrationProjecting,
		PlacementMigrationReadyToCutover,
		PlacementMigrationCutover,
		PlacementMigrationRetiring,
	} {
		if !phase.CanTransitionTo(PlacementMigrationFailed) {
			t.Fatalf("%s must be able to fail", phase)
		}
		if !phase.CanTransitionTo(phase) {
			t.Fatalf("%s must allow idempotent refresh", phase)
		}
	}
	if PlacementMigrationSucceeded.CanTransitionTo(PlacementMigrationFailed) {
		t.Fatal("succeeded migration must be immutable")
	}
	if PlacementMigrationFailed.CanTransitionTo(PlacementMigrationRequested) {
		t.Fatal("failed migration must be immutable")
	}
}
