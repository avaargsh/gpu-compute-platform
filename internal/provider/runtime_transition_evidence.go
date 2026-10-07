package provider

import (
	"fmt"
	"strings"
)

// RuntimeTransitionEvidence is a provider-neutral falsification envelope for
// experimental runtime transitions such as prefill/decode role switching.
// Provider-native transfer details remain opaque EvidenceRefs.
type RuntimeTransitionEvidence struct {
	Provider               string
	RuntimeVersion         string
	ImageDigest            string
	WorkloadID             string
	Generation             int64
	AttemptID              string
	RequestedRole          string
	ObservedRole           string
	TransportSucceeded     bool
	RuntimeTerminalSuccess bool
	SemanticVerified       bool
	SemanticReference      string
	LeaseOwner             string
	ChaosCase              string
	EvidenceRefs           []string
}

func (e RuntimeTransitionEvidence) ValidateForAcceptance() error {
	required := map[string]string{
		"provider": e.Provider, "runtime version": e.RuntimeVersion,
		"image digest": e.ImageDigest, "workload id": e.WorkloadID,
		"attempt id": e.AttemptID, "requested role": e.RequestedRole,
		"observed role": e.ObservedRole, "semantic reference": e.SemanticReference,
		"lease owner": e.LeaseOwner, "chaos case": e.ChaosCase,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if e.Generation <= 0 {
		return fmt.Errorf("generation must be positive")
	}
	if !strings.HasPrefix(e.ImageDigest, "sha256:") || len(strings.TrimPrefix(e.ImageDigest, "sha256:")) != 64 {
		return fmt.Errorf("image digest must be an immutable sha256 digest")
	}
	if e.RequestedRole != e.ObservedRole {
		return fmt.Errorf("runtime role mismatch: requested %q observed %q", e.RequestedRole, e.ObservedRole)
	}
	if !e.TransportSucceeded {
		return fmt.Errorf("transport did not succeed")
	}
	if !e.RuntimeTerminalSuccess {
		return fmt.Errorf("runtime did not reach terminal success")
	}
	if !e.SemanticVerified {
		return fmt.Errorf("semantic verification failed")
	}
	if len(e.EvidenceRefs) == 0 {
		return fmt.Errorf("provider evidence refs are required")
	}
	return nil
}
