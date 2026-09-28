package agentstore

import (
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
)

func TestMergeConditionsPreservesTransitionTimeWhenStatusIsStable(t *testing.T) {
	oldTime := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)

	got := MergeConditions(
		[]domain.Condition{{Type: "Ready", Status: "False", Reason: "Pending", LastTransitionTime: oldTime}},
		[]domain.Condition{{Type: "Ready", Status: "False", Reason: "AwaitingPods", Message: "changed", LastTransitionTime: newTime}},
	)
	if len(got) != 1 {
		t.Fatalf("conditions=%#v", got)
	}
	if !got[0].LastTransitionTime.Equal(oldTime) {
		t.Fatalf("stable status changed transition time: %#v", got[0])
	}
	if got[0].Reason != "AwaitingPods" || got[0].Message != "changed" {
		t.Fatalf("current reason/message must be preserved: %#v", got[0])
	}
}

func TestMergeConditionsUsesCurrentTimeOnStatusTransition(t *testing.T) {
	oldTime := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)

	got := MergeConditions(
		[]domain.Condition{{Type: "Ready", Status: "False", LastTransitionTime: oldTime}},
		[]domain.Condition{{Type: "Ready", Status: "True", LastTransitionTime: newTime}},
	)
	if len(got) != 1 || !got[0].LastTransitionTime.Equal(newTime) {
		t.Fatalf("transition must use current timestamp: %#v", got)
	}
}
