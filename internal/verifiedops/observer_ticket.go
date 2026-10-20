package verifiedops

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// An Observer work ticket is signed by an independently authenticated
// Control Plane dispatcher AFTER PostgreSQL issues the matching challenge.
// The signature authenticates the requested read scope, not a Job outcome.
const ObserverWorkTicketPayloadType = "application/vnd.gpupanel.observer-work+json"
const ObserverIdentityUsername = "system:serviceaccount:verifiedops-observer:verifiedops-observer"
const observerWorkTicketMaxTTL = 2 * time.Minute

type ObserverWorkTicket struct {
	Challenge ObserverSubject `json:"challenge"`
	Expected  ExpectedJob     `json:"expectedJob"`
	IssuedAt  string          `json:"issuedAt"`
	ExpiresAt string          `json:"expiresAt"`
}

type ApprovedObserverWork struct {
	ticket    ObserverWorkTicket
	expiresAt time.Time
}

// AuthorObserverWorkTicket accepts an externally provided crypto.Signer.
// Do not call it with a request scope taken from the browser or cluster Agent;
// the caller must get the original Operation and ACK-bound Job from storage.
func AuthorObserverWorkTicket(ticket ObserverWorkTicket, identity ObserverSigner) ([]byte, error) {
	if err := checkWorkTicket(ticket, time.Now().UTC()); err != nil {
		return nil, err
	}
	if identity.KeyID == "" || identity.Signer == nil {
		return nil, ErrObserverUntrusted
	}
	pub, ok := identity.Signer.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, ErrObserverUntrusted
	}
	payload, err := json.Marshal(ticket)
	if err != nil {
		return nil, err
	}
	pae := dssePAE(ObserverWorkTicketPayloadType, payload)
	signature, err := identity.Signer.Sign(rand.Reader, pae, crypto.Hash(0))
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, pae, signature) {
		return nil, ErrObserverUntrusted
	}
	return json.Marshal(SignedObserverEnvelope{
		PayloadType: ObserverWorkTicketPayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures:  []ObserverSignature{{KeyID: identity.KeyID, Sig: base64.StdEncoding.EncodeToString(signature)}},
	})
}

// ApproveObserverWorkTicket requires an out-of-band, pinned Control Plane
// public key. Unauthenticated JSON and unknown/duplicate fields fail closed.
func ApproveObserverWorkTicket(raw []byte, root ObserverTrust) (ApprovedObserverWork, error) {
	var zero ApprovedObserverWork
	if len(raw) == 0 || len(raw) > 1<<20 || root.KeyID == "" || len(root.PublicKey) != ed25519.PublicKeySize {
		return zero, ErrObserverUntrusted
	}
	var envelope SignedObserverEnvelope
	if err := strictObserverJSON(raw, &envelope); err != nil || envelope.PayloadType != ObserverWorkTicketPayloadType ||
		len(envelope.Signatures) != 1 || envelope.Signatures[0].KeyID != root.KeyID {
		return zero, ErrObserverUntrusted
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(envelope.Payload)
	if err != nil || len(payload) == 0 || len(payload) > 1<<19 {
		return zero, ErrObserverUntrusted
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(envelope.Signatures[0].Sig)
	if err != nil || !ed25519.Verify(root.PublicKey, dssePAE(envelope.PayloadType, payload), signature) {
		return zero, ErrObserverUntrusted
	}
	var ticket ObserverWorkTicket
	if err := strictObserverJSON(payload, &ticket); err != nil {
		return zero, ErrObserverUntrusted
	}
	if err := checkWorkTicket(ticket, time.Now().UTC()); err != nil {
		return zero, ErrObserverUntrusted
	}
	expiry, _ := time.Parse(time.RFC3339Nano, ticket.ExpiresAt)
	return ApprovedObserverWork{ticket: ticket, expiresAt: expiry}, nil
}

func checkWorkTicket(ticket ObserverWorkTicket, now time.Time) error {
	if !validExpected(ticket.Expected) || !validJobChallenge(ticket.Challenge, ticket.Expected) {
		return ErrObserverUntrusted
	}
	issued, err := time.Parse(time.RFC3339Nano, ticket.IssuedAt)
	if err != nil {
		return ErrObserverUntrusted
	}
	expiry, err := time.Parse(time.RFC3339Nano, ticket.ExpiresAt)
	if err != nil || !expiry.After(issued) || expiry.Sub(issued) > observerWorkTicketMaxTTL ||
		now.After(expiry) || issued.After(now.Add(30*time.Second)) {
		return ErrObserverUntrusted
	}
	return nil
}

// RunApprovedObserverWork only accepts a previously verified dispatcher
// ticket and a distinct read-only Kubernetes ServiceAccount identity.
// The ticket's expiry is checked again after Kubernetes reads to prevent a
// delayed caller from signing a stale approval. This does not supply an
// end-user authorization verdict or attest the signer deployment integrity.
func RunApprovedObserverWork(ctx context.Context, client kubernetes.Interface,
	work ApprovedObserverWork, observerKey ObserverSigner,
) ([]byte, Observation, error) {
	if client == nil || !work.expiresAt.After(time.Now().UTC()) ||
		checkWorkTicket(work.ticket, time.Now().UTC()) != nil {
		return nil, Observation{}, ErrObserverUntrusted
	}
	self, err := client.AuthenticationV1().SelfSubjectReviews().Create(ctx,
		&authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil || self == nil || self.Status.UserInfo.Username != ObserverIdentityUsername {
		return nil, Observation{}, fmt.Errorf("independent observer identity not proven: %w", ErrObserverUntrusted)
	}
	// This call only performs GET/LIST/GET reads and signs that fresh snapshot.
	proof, observed, err := NewJobObserver(client).ObserveAndSign(ctx,
		work.ticket.Challenge, work.ticket.Expected, observerKey)
	if err != nil {
		return nil, observed, err
	}
	if !work.expiresAt.After(time.Now().UTC()) {
		return nil, Observation{}, ErrObserverUntrusted
	}
	return proof, observed, nil
}
