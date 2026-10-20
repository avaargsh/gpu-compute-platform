package verifiedops

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Versioned separately from the earlier v1 nonce-receipt predicate. The
// original v1 verifier must never claim to verify this observation body.
const ObservedJobPredicateType = "https://gpupanel.dev/attestation/observed-job/v1"

type SignedExpectedJob struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	JobUID        string `json:"jobUid"`
	Generation    int64  `json:"generation"`
	ContainerName string `json:"containerName"`
	ImageDigest   string `json:"imageDigest"`
}

func signedExpected(w ExpectedJob) SignedExpectedJob {
	return SignedExpectedJob{Namespace: w.Namespace, Name: w.Name, JobUID: string(w.JobUID),
		Generation: w.Generation, ContainerName: w.ContainerName, ImageDigest: w.ImageDigest}
}

type ObservedJobPredicate struct {
	Challenge   ObserverSubject   `json:"challenge"`
	Expected    SignedExpectedJob `json:"expectedJob"`
	Observation Observation       `json:"observation"`
}
type ObservedJobSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}
type ObservedJobStatement struct {
	Type          string               `json:"_type"`
	PredicateType string               `json:"predicateType"`
	Subject       []ObservedJobSubject `json:"subject"`
	Predicate     ObservedJobPredicate `json:"predicate"`
}
type ObserverSigner struct {
	KeyID  string
	Signer crypto.Signer
}
type VerifiedObservedJob struct {
	Subject     ObserverSubject
	Expected    SignedExpectedJob
	Observation Observation
}

// A challenge is an authority INPUT from a trusted Control Plane, not a
// grant from the Observer. Never copy arbitrary client fields here.
func validJobChallenge(c ObserverSubject, w ExpectedJob) bool {
	return bindingOperationIDForObserver(c.OperationID) && c.ClusterID != "" &&
		c.ProjectID != "" && c.WorkloadID != "" && c.Generation == w.Generation &&
		c.JobUID == string(w.JobUID) && c.ObservedAt == "" &&
		len(c.Nonce) == 64 && strings.Trim(c.Nonce, "0123456789abcdef") == ""
}
func bindingOperationIDForObserver(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}

func consistentObservation(o Observation, w ExpectedJob) bool {
	switch o.Code {
	case OutcomePending, OwnershipUnproven, OutcomeMismatch:
		return true
	case PODReadyObserved:
		return o.JobUID == w.JobUID && o.JobResourceVersion != "" &&
			o.PodUID != "" && o.PodResourceVersion != "" &&
			o.NodeUID != "" && o.NodeResourceVersion != "" &&
			o.NodeName != "" && strings.HasSuffix(o.ImageID, "@"+w.ImageDigest)
	default:
		return false
	}
}

// ObserveAndSign signs only fields read through this Observer's Kubernetes
// client. No private key, signing token or production key distribution is
// supplied by this package: crypto.Signer is injected by independent caller.
func (o *JobObserver) ObserveAndSign(ctx context.Context, challenge ObserverSubject,
	expected ExpectedJob, signer ObserverSigner) ([]byte, Observation, error) {
	if !validExpected(expected) || !validJobChallenge(challenge, expected) ||
		signer.KeyID == "" || signer.Signer == nil {
		return nil, Observation{}, ErrObserverUntrusted
	}
	pub, ok := signer.Signer.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, Observation{}, ErrObserverUntrusted
	}
	observed, err := o.Observe(ctx, expected)
	if err != nil || observed.Code == VerificationUnavailable {
		return nil, observed, fmt.Errorf("observer could not get trustworthy fresh read: %w", ErrObserverUntrusted)
	}
	if !consistentObservation(observed, expected) {
		return nil, observed, ErrObserverUntrusted
	}
	challenge.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	p := ObservedJobPredicate{Challenge: challenge, Expected: signedExpected(expected), Observation: observed}
	canonical, err := json.Marshal(p)
	if err != nil {
		return nil, observed, err
	}
	digest := sha256.Sum256(canonical)
	st := ObservedJobStatement{
		Type: ObserverStatementType, PredicateType: ObservedJobPredicateType,
		Subject: []ObservedJobSubject{{Name: "k8s-job:" + string(expected.JobUID),
			Digest: map[string]string{"sha256": hex.EncodeToString(digest[:])}}},
		Predicate: p,
	}
	payload, err := json.Marshal(st)
	if err != nil {
		return nil, observed, err
	}
	pae := dssePAE(ObserverPayloadType, payload)
	sig, err := signer.Signer.Sign(rand.Reader, pae, crypto.Hash(0))
	if err != nil {
		return nil, observed, err
	}
	if !ed25519.Verify(pub, pae, sig) {
		return nil, observed, ErrObserverUntrusted
	}
	envelope, err := json.Marshal(SignedObserverEnvelope{
		PayloadType: ObserverPayloadType, Payload: base64.StdEncoding.EncodeToString(payload),
		Signatures: []ObserverSignature{{KeyID: signer.KeyID, Sig: base64.StdEncoding.EncodeToString(sig)}},
	})
	return envelope, observed, err
}

// VerifyObservedJobEnvelope authenticates the signed bytes and exact
// Challenge/ExpectedJob/Observation digest against an independently pinned
// public key. It does NOT verify signer deployment identity, actual runtime
// truth, CREATE ownership, or an authorized outcome.
func VerifyObservedJobEnvelope(raw []byte, trust ObserverTrust,
	challenge ObserverSubject, expected ExpectedJob) (VerifiedObservedJob, error) {
	var zero VerifiedObservedJob
	if len(raw) == 0 || len(raw) > 1<<20 || trust.KeyID == "" ||
		len(trust.PublicKey) != ed25519.PublicKeySize ||
		!validExpected(expected) || !validJobChallenge(challenge, expected) {
		return zero, ErrObserverUntrusted
	}
	var env SignedObserverEnvelope
	if strictObserverJSON(raw, &env) != nil || env.PayloadType != ObserverPayloadType ||
		len(env.Signatures) != 1 || env.Signatures[0].KeyID != trust.KeyID {
		return zero, ErrObserverUntrusted
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(env.Payload)
	if err != nil || len(payload) == 0 || len(payload) > 1<<19 {
		return zero, ErrObserverUntrusted
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(env.Signatures[0].Sig)
	if err != nil || !ed25519.Verify(trust.PublicKey, dssePAE(env.PayloadType, payload), sig) {
		return zero, ErrObserverUntrusted
	}
	var st ObservedJobStatement
	if strictObserverJSON(payload, &st) != nil || st.Type != ObserverStatementType ||
		st.PredicateType != ObservedJobPredicateType || len(st.Subject) != 1 ||
		len(st.Subject[0].Digest) != 1 || st.Subject[0].Name != "k8s-job:"+string(expected.JobUID) {
		return zero, ErrObserverUntrusted
	}
	p := st.Predicate
	at, err := time.Parse(time.RFC3339Nano, p.Challenge.ObservedAt)
	now := time.Now().UTC()
	if err != nil || at.Before(now.Add(-5*time.Minute)) || at.After(now.Add(30*time.Second)) {
		return zero, ErrObserverUntrusted
	}
	signedAt := p.Challenge.ObservedAt
	p.Challenge.ObservedAt = ""
	if p.Challenge != challenge || p.Expected != signedExpected(expected) ||
		!consistentObservation(p.Observation, expected) {
		return zero, ErrObserverUntrusted
	}
	p.Challenge.ObservedAt = signedAt
	b, err := json.Marshal(p)
	if err != nil {
		return zero, ErrObserverUntrusted
	}
	sum := sha256.Sum256(b)
	if st.Subject[0].Digest["sha256"] != hex.EncodeToString(sum[:]) {
		return zero, ErrObserverUntrusted
	}
	return VerifiedObservedJob{Subject: p.Challenge, Expected: p.Expected, Observation: p.Observation}, nil
}
