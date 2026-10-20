package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

const maxObserverEvidenceEnvelopeBytes = 1 << 20

// ObserverEvidenceRunner is implemented by a trusted, reviewed delivery
// adapter. Implementations must deliver the ticket only to the isolated
// read-only Observer and return its raw DSSE envelope; they must not accept
// scope, trust roots, or receipt decisions from the Observer itself.
type ObserverEvidenceRunner interface {
	RunObserver(ctx context.Context, signedTicket []byte) ([]byte, error)
}

// DispatchAndRecordSignedObservedJob is the narrow trusted orchestration seam
// joining the existing database ticket issuer, an independently isolated
// Observer runner, and the PostgreSQL receipt admission transaction.
//
// scope, dispatcher signer and Observer key ID MUST be resolved by trusted
// server-side configuration/orchestration, never from an HTTP request or
// unsigned Agent/Observer payload. This method does not expose an endpoint,
// configure key custody, prove the adapter's deployment identity, or issue an
// AUTHORIZED_AND_VERIFIED verdict.
func (s *Store) DispatchAndRecordSignedObservedJob(
	ctx context.Context,
	scope ObserverChallengeScope,
	dispatcherSigner verifiedops.ObserverSigner,
	observerKeyID string,
	runner ObserverEvidenceRunner,
) (ObservedJobReceipt, error) {
	var empty ObservedJobReceipt
	if s == nil || s.db == nil {
		return empty, fmt.Errorf("postgres database is required")
	}
	if runner == nil || dispatcherSigner.Signer == nil ||
		dispatcherSigner.KeyID == "" || strings.TrimSpace(observerKeyID) == "" || len(observerKeyID) > 128 {
		return empty, verifiedops.ErrObserverUntrusted
	}

	// This call derives ExpectedJob and persists a short-lived challenge from
	// durable Desired + original CREATE identity. Caller-provided scope is
	// revalidated against those records before any ticket can be signed.
	issued, err := s.IssueSignedObserverWork(ctx, scope, dispatcherSigner)
	if err != nil {
		return empty, fmt.Errorf("issue trusted Observer work ticket: %w", err)
	}
	if len(issued.Ticket) == 0 || len(issued.Ticket) > maxObserverEvidenceEnvelopeBytes {
		return empty, verifiedops.ErrObserverUntrusted
	}

	// The runner is a transport boundary, not an authority boundary. Its
	// output remains untrusted until the receipt method verifies DSSE, signer,
	// exact operation/UID/image binding, challenge freshness and replay state.
	envelope, err := runner.RunObserver(ctx, append([]byte(nil), issued.Ticket...))
	if err != nil {
		return empty, fmt.Errorf("run isolated Observer: %w", err)
	}
	if len(envelope) == 0 || len(envelope) > maxObserverEvidenceEnvelopeBytes {
		return empty, verifiedops.ErrObserverUntrusted
	}
	receipt, err := s.RecordSignedObservedJobProof(
		ctx, envelope, observerKeyID, scope, issued.Challenge.Nonce)
	if err != nil {
		return empty, fmt.Errorf("admit signed Observer evidence: %w", err)
	}
	return receipt, nil
}
