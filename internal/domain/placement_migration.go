package domain

type PlacementMigrationPhase string

const (
	PlacementMigrationRequested      PlacementMigrationPhase = "Requested"
	PlacementMigrationProjecting     PlacementMigrationPhase = "Projecting"
	PlacementMigrationReadyToCutover PlacementMigrationPhase = "ReadyToCutover"
	PlacementMigrationCutover        PlacementMigrationPhase = "Cutover"
	PlacementMigrationRetiring       PlacementMigrationPhase = "Retiring"
	PlacementMigrationSucceeded      PlacementMigrationPhase = "Succeeded"
	PlacementMigrationFailed         PlacementMigrationPhase = "Failed"
)

type PlacementMigration struct {
	Metadata        Metadata                `json:"metadata"`
	PoolID          ID                      `json:"poolId"`
	SourceClusterID ID                      `json:"sourceClusterId"`
	TargetClusterID ID                      `json:"targetClusterId"`
	Phase           PlacementMigrationPhase `json:"phase"`
	Conditions      []Condition             `json:"conditions,omitempty"`
	EvidenceRefs    []string                `json:"evidenceRefs,omitempty"`
}

func (m PlacementMigration) ValidateRequest() bool {
	return m.Metadata.ID != "" &&
		m.PoolID != "" &&
		m.SourceClusterID != "" &&
		m.TargetClusterID != "" &&
		m.SourceClusterID != m.TargetClusterID &&
		m.Phase == PlacementMigrationRequested
}

func (p PlacementMigrationPhase) Terminal() bool {
	return p == PlacementMigrationSucceeded || p == PlacementMigrationFailed
}
