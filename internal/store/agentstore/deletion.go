package agentstore

import "github.com/avaargsh/gpu-compute-platform/internal/agent"

// Finalization consumes evidence only after cleanup has observed provider absence.
func HasDeletionEvidence(observation agent.Observation, generation int64) bool {
	if observation.ObservedGeneration != generation || len(observation.EvidenceRefs) == 0 {
		return false
	}
	for _, ref := range observation.EvidenceRefs {
		if ref == "" {
			return false
		}
	}
	for _, condition := range observation.Conditions {
		if condition.Type == "Ready" && condition.Status == "False" && condition.Reason == "Deleted" {
			return true
		}
	}
	return false
}
