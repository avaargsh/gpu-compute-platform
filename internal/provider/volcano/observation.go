package volcano

import (
	"fmt"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

type WorkloadState struct {
	Phase        string
	Admitted     bool
	PodsReady    bool
	Succeeded    bool
	Failed       bool
	Message      string
	JobRef       string
	PodGroupRef  string
	PodRefs      []string
}

func TranslateWorkloadObservation(generation int64, state WorkloadState) (baseprovider.WorkloadObservation, error) {
	if generation <= 0 {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("observed generation must be positive")
	}
	if state.JobRef == "" {
		return baseprovider.WorkloadObservation{}, fmt.Errorf("VolcanoJob evidence is required")
	}
	now := time.Now().UTC()
	ready := (state.PodsReady || state.Succeeded) && !state.Failed
	conditions := []domain.Condition{
		{Type: "Ready", Status: boolStatus(ready), Reason: volcanoReadyReason(state), Message: state.Message, LastTransitionTime: now},
		{Type: "Admitted", Status: boolStatus(state.Admitted), Reason: conditionReason(state.Admitted, "VolcanoAdmitted", "AwaitingVolcanoAdmission"), LastTransitionTime: now},
		{Type: "PodsReady", Status: boolStatus(state.PodsReady), Reason: conditionReason(state.PodsReady, "PodsReady", "AwaitingPods"), LastTransitionTime: now},
	}
	if state.Succeeded {
		conditions = append(conditions, domain.Condition{Type: "Succeeded", Status: "True", Reason: "VolcanoJobSucceeded", LastTransitionTime: now})
	}
	if state.Failed {
		conditions = append(conditions, domain.Condition{Type: "Failed", Status: "True", Reason: "VolcanoJobFailed", Message: state.Message, LastTransitionTime: now})
	}
	evidence := []string{state.JobRef}
	if state.PodGroupRef != "" { evidence = append(evidence, state.PodGroupRef) }
	evidence = append(evidence, state.PodRefs...)
	return baseprovider.WorkloadObservation{ObservedGeneration: generation, Phase: state.Phase, Conditions: conditions, EvidenceRefs: evidence}, nil
}

func boolStatus(v bool) string { if v { return "True" }; return "False" }
func conditionReason(v bool, yes, no string) string { if v { return yes }; return no }
func volcanoReadyReason(s WorkloadState) string {
	switch {
	case s.Failed: return "VolcanoJobFailed"
	case s.Succeeded: return "VolcanoJobSucceeded"
	case s.PodsReady: return "PodsReady"
	case s.Admitted: return "AwaitingPods"
	default: return "Pending"
	}
}
