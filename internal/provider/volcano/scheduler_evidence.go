package volcano

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// podGroupSchedulerEvidence is a deliberately non-promoting interpretation of
// the version-pinned Volcano scheduler PodGroup condition vocabulary.
// It cannot establish scheduler-applied Queue quota, a durable admission
// decision, or operation ownership from one PodGroup snapshot.
type podGroupSchedulerEvidence struct {
	Phase     string
	Scheduled string
	Reason    string
}

func inspectPodGroupSchedulerEvidence(group *unstructured.Unstructured) podGroupSchedulerEvidence {
	result := podGroupSchedulerEvidence{
		Phase: "Unknown", Scheduled: "Unknown", Reason: "VolcanoSchedulerEvidenceUnavailable",
	}
	if group == nil || group.GetUID() == "" || group.GetResourceVersion() == "" ||
		group.GetDeletionTimestamp() != nil {
		return result
	}
	phase, found, err := unstructured.NestedString(group.Object, "status", "phase")
	if err != nil || !found {
		return result
	}
	switch phase {
	case "Pending", "Inqueue", "Running", "Unknown", "Completed":
		result.Phase = phase
	default:
		result.Reason = "UnreviewedVolcanoPodGroupPhase"
		return result
	}
	raw, found, err := unstructured.NestedSlice(group.Object, "status", "conditions")
	if err != nil || !found || len(raw) == 0 {
		result.Reason = "VolcanoScheduledConditionUnavailable"
		return result
	}
	scheduled := 0
	unschedulableTrue := false
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			result.Reason = "MalformedVolcanoSchedulerCondition"
			return result
		}
		switch item["type"] {
		case "Scheduled":
			scheduled++
			if scheduled > 1 {
				result.Reason = "AmbiguousVolcanoScheduledConditions"
				return result
			}
			status, ok := item["status"].(string)
			if !ok || (status != "True" && status != "False" && status != "Unknown") {
				result.Reason = "MalformedVolcanoSchedulerCondition"
				return result
			}
			result.Scheduled = status
		case "Unschedulable":
			if item["status"] == "True" {
				unschedulableTrue = true
			}
		}
	}
	if scheduled != 1 {
		result.Reason = "VolcanoScheduledConditionUnavailable"
		return result
	}
	if result.Scheduled == "True" && unschedulableTrue {
		result.Scheduled = "Unknown"
		result.Reason = "ConflictingVolcanoSchedulerConditions"
		return result
	}
	// A Running phase describes the number of running Pods. Neither this
	// phase nor Scheduled=True proves a Queue generation was applied.
	if result.Scheduled == "True" {
		result.Reason = "VolcanoScheduledConditionObserved"
	} else {
		result.Reason = fmt.Sprintf("VolcanoScheduledCondition%s", result.Scheduled)
	}
	return result
}
