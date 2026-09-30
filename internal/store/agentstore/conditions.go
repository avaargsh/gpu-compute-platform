package agentstore

import "github.com/avaargsh/gpu-compute-platform/internal/domain"

func MergeConditions(previous, current []domain.Condition) []domain.Condition {
	if len(current) == 0 {
		return nil
	}
	byType := make(map[string]domain.Condition, len(previous))
	for _, condition := range previous {
		byType[condition.Type] = condition
	}
	out := make([]domain.Condition, len(current))
	for i, condition := range current {
		if old, ok := byType[condition.Type]; ok && old.Status == condition.Status && !old.LastTransitionTime.IsZero() {
			condition.LastTransitionTime = old.LastTransitionTime
		}
		out[i] = condition
	}
	return out
}
