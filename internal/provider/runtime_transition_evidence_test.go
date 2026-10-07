package provider

import "testing"

func validRuntimeTransitionEvidence() RuntimeTransitionEvidence {
	return RuntimeTransitionEvidence{
		Provider: "sglang", RuntimeVersion: "test-build",
		ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkloadID: "workload-a", Generation: 7, AttemptID: "attempt-1",
		RequestedRole: "decode", ObservedRole: "decode",
		TransportSucceeded: true, RuntimeTerminalSuccess: true,
		SemanticVerified: true, SemanticReference: "aifactory://evidence/pd-1",
		LeaseOwner: "agent-a", ChaosCase: "PD-C1",
		EvidenceRefs: []string{"provider://sglang/attempt/attempt-1"},
	}
}

func TestRuntimeTransitionEvidenceAcceptsCompleteProof(t *testing.T) {
	if err := validRuntimeTransitionEvidence().ValidateForAcceptance(); err != nil {
		t.Fatalf("expected evidence to pass: %v", err)
	}
}

func TestRuntimeTransitionEvidenceRejectsHTTPStyleSuccessWithoutSemanticProof(t *testing.T) {
	in := validRuntimeTransitionEvidence()
	in.SemanticVerified = false
	if err := in.ValidateForAcceptance(); err == nil {
		t.Fatal("expected semantic verification failure")
	}
}

func TestRuntimeTransitionEvidenceRejectsRoleMismatch(t *testing.T) {
	in := validRuntimeTransitionEvidence()
	in.ObservedRole = "prefill"
	if err := in.ValidateForAcceptance(); err == nil {
		t.Fatal("expected role mismatch to fail closed")
	}
}

func TestRuntimeTransitionEvidenceRequiresImmutableImageDigest(t *testing.T) {
	in := validRuntimeTransitionEvidence()
	in.ImageDigest = "sglang:latest"
	if err := in.ValidateForAcceptance(); err == nil {
		t.Fatal("expected mutable image reference to fail")
	}
}

func TestRuntimeTransitionEvidenceRequiresProviderEvidence(t *testing.T) {
	in := validRuntimeTransitionEvidence()
	in.EvidenceRefs = nil
	if err := in.ValidateForAcceptance(); err == nil {
		t.Fatal("expected missing evidence refs to fail")
	}
}

func TestRuntimeTransitionEvidenceRejectsLocalSemanticReference(t *testing.T) {
	in := validRuntimeTransitionEvidence()
	in.SemanticReference = "reference://fixture/pd-1"
	if err := in.ValidateForAcceptance(); err == nil {
		t.Fatal("expected non-AI-Factory semantic reference to fail")
	}
}

func TestRuntimeTransitionChaosMatrixFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RuntimeTransitionEvidence)
	}{
		{"PD-C1-component-reorder-incomplete", func(in *RuntimeTransitionEvidence) { in.RuntimeTerminalSuccess = false }},
		{"PD-C2-missing-component", func(in *RuntimeTransitionEvidence) { in.TransportSucceeded = false }},
		{"PD-C3-empty-transfer-stuck-role", func(in *RuntimeTransitionEvidence) { in.ObservedRole = "" }},
		{"PD-C4-chunk-boundary-semantic-mismatch", func(in *RuntimeTransitionEvidence) { in.SemanticVerified = false }},
		{"PD-C5-layout-mismatch", func(in *RuntimeTransitionEvidence) { in.RuntimeTerminalSuccess = false }},
		{"PD-C6-process-death-no-terminal-proof", func(in *RuntimeTransitionEvidence) { in.RuntimeTerminalSuccess = false }},
		{"PD-C7-lost-observation-no-lease-owner", func(in *RuntimeTransitionEvidence) { in.LeaseOwner = "" }},
		{"PD-C8-http-200-silent-corruption", func(in *RuntimeTransitionEvidence) {
			in.TransportSucceeded = true
			in.RuntimeTerminalSuccess = true
			in.SemanticVerified = false
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validRuntimeTransitionEvidence()
			in.ChaosCase = tc.name[:5]
			tc.mutate(&in)
			if err := in.ValidateForAcceptance(); err == nil {
				t.Fatal("expected chaos case to fail closed")
			}
		})
	}
}
