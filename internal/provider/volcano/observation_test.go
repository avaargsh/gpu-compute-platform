package volcano

import "testing"

func conditionStatus(t *testing.T, in WorkloadState, typ string) string {
	t.Helper()
	obs, err := TranslateWorkloadObservation(7, in)
	if err != nil { t.Fatal(err) }
	for _, c := range obs.Conditions { if c.Type == typ { return c.Status } }
	return ""
}

func TestTranslateWorkloadObservationDoesNotInventQuotaReserved(t *testing.T) {
	obs, err := TranslateWorkloadObservation(7, WorkloadState{JobRef: "volcano://cluster/ns/jobs/j", Admitted: true})
	if err != nil { t.Fatal(err) }
	for _, c := range obs.Conditions {
		if c.Type == "QuotaReserved" { t.Fatal("Volcano observation must not invent Kueue reservation semantics") }
	}
}

func TestTranslateWorkloadObservationRequiresWorkloadEvidenceForReady(t *testing.T) {
	if got := conditionStatus(t, WorkloadState{JobRef: "volcano://cluster/ns/jobs/j", Admitted: true}, "Ready"); got != "False" {
		t.Fatalf("Ready=%s, want False before pod/success evidence", got)
	}
	if got := conditionStatus(t, WorkloadState{JobRef: "volcano://cluster/ns/jobs/j", Admitted: true, PodsReady: true}, "Ready"); got != "True" {
		t.Fatalf("Ready=%s, want True with ready pod evidence", got)
	}
}

func TestTranslateWorkloadObservationCarriesChildEvidence(t *testing.T) {
	obs, err := TranslateWorkloadObservation(7, WorkloadState{
		JobRef: "volcano://cluster/ns/jobs/j",
		PodGroupRef: "volcano://cluster/ns/podgroups/pg",
		PodRefs: []string{"k8s://cluster/namespaces/ns/pods/p1"},
	})
	if err != nil { t.Fatal(err) }
	if len(obs.EvidenceRefs) != 3 { t.Fatalf("evidence=%v", obs.EvidenceRefs) }
}

func TestTranslateWorkloadObservationFailsWithoutVolcanoJobEvidence(t *testing.T) {
	if _, err := TranslateWorkloadObservation(7, WorkloadState{PodsReady: true}); err == nil {
		t.Fatal("expected missing VolcanoJob evidence to fail closed")
	}
}
