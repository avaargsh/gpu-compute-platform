package volcano

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestVolcanoSchedulerEvidenceIsNeverQuotaProof(t *testing.T) {
	tests := []struct {
		name, phase, scheduled, expected, reason string
		unschedulable                            bool
		duplicate                                bool
	}{
		{"running without scheduled condition", "Running", "", "Unknown", "VolcanoScheduledConditionUnavailable", false, false},
		{"scheduled true", "Running", "True", "True", "VolcanoScheduledConditionObserved", false, false},
		{"scheduled true but only Inqueue", "Inqueue", "True", "True", "VolcanoScheduledConditionObserved", false, false},
		{"pending and scheduled false", "Pending", "False", "False", "VolcanoScheduledConditionFalse", false, false},
		{"unknown scheduled status", "Unknown", "Unknown", "Unknown", "VolcanoScheduledConditionUnknown", false, false},
		{"contradictory unschedulable true", "Running", "True", "Unknown", "ConflictingVolcanoSchedulerConditions", true, false},
		{"duplicate scheduled", "Running", "True", "Unknown", "AmbiguousVolcanoScheduledConditions", false, true},
		{"unreviewed phase", "Deleting", "True", "Unknown", "UnreviewedVolcanoPodGroupPhase", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conditions := []any{}
			if tt.scheduled != "" {
				conditions = append(conditions, map[string]any{"type": "Scheduled", "status": tt.scheduled})
			}
			if tt.duplicate {
				conditions = append(conditions, map[string]any{"type": "Scheduled", "status": tt.scheduled})
			}
			if tt.unschedulable {
				conditions = append(conditions, map[string]any{"type": "Unschedulable", "status": "True"})
			}
			pg := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "scheduling.volcano.sh/v1beta1",
				"kind":       "PodGroup",
				"metadata": map[string]any{
					"name": "a", "namespace": "b", "uid": "pg-uid", "resourceVersion": "41",
				},
				"status": map[string]any{"phase": tt.phase, "conditions": conditions},
			}}
			got := inspectPodGroupSchedulerEvidence(pg)
			if got.Scheduled != tt.expected || got.Reason != tt.reason {
				t.Fatalf("result=%#v expected scheduled=%s reason=%s", got, tt.expected, tt.reason)
			}
			if got.Phase != tt.phase && tt.phase != "Deleting" {
				t.Fatalf("phase=%q expected=%q", got.Phase, tt.phase)
			}
		})
	}
}

func TestVolcanoSchedulerEvidenceRequiresServerIdentity(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*unstructured.Unstructured)
	}{
		{"missing UID", func(pg *unstructured.Unstructured) { pg.SetUID("") }},
		{"missing RV", func(pg *unstructured.Unstructured) { pg.SetResourceVersion("") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			pg := &unstructured.Unstructured{Object: map[string]any{
				"metadata": map[string]any{"uid": "uid", "resourceVersion": "41"},
				"status": map[string]any{
					"phase":      "Running",
					"conditions": []any{map[string]any{"type": "Scheduled", "status": "True"}},
				},
			}}
			change.apply(pg)
			got := inspectPodGroupSchedulerEvidence(pg)
			if got.Scheduled != "Unknown" || got.Reason != "VolcanoSchedulerEvidenceUnavailable" {
				t.Fatalf("unidentified object produced evidence: %#v", got)
			}
		})
	}
}
