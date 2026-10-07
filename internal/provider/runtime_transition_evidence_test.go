package provider

import "testing"

func validRuntimeTransitionEvidence() RuntimeTransitionEvidence {
	return RuntimeTransitionEvidence{
		Provider: "sglang", RuntimeVersion: "test-build",
		ImageDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkloadID: "workload-a", Generation: 7, AttemptID: "attempt-1",
		RequestedRole: "decode", ObservedRole: "decode",
		TransportSucceeded: true, RuntimeTerminalSuccess: true,
		SemanticVerified: true, SemanticReference: "reference://fixture/pd-1",
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
