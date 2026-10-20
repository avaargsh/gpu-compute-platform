package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

func signedNonceFixture(t *testing.T, in verifiedops.ObserverSubject) ([]byte, verifiedops.ObserverTrust) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	statement := verifiedops.ObserverStatement{
		Type:          verifiedops.ObserverStatementType,
		PredicateType: verifiedops.ObserverPredicateType,
		Predicate:     in,
	}
	statement.Subject = append(statement.Subject, struct {
		Name   string            `json:"name"`
		Digest map[string]string `json:"digest"`
	}{
		Name:   "k8s-job:" + in.JobUID,
		Digest: map[string]string{"sha256": strings.Repeat("a", 64)},
	})
	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	// DSSE PAE is derived from the exact unsigned in-toto JSON bytes.
	pae := []byte("DSSEv1 " + fmt.Sprint(len(verifiedops.ObserverPayloadType)) +
		" " + verifiedops.ObserverPayloadType + " " +
		fmt.Sprint(len(payload)) + " " + string(payload))
	env := verifiedops.SignedObserverEnvelope{
		PayloadType: verifiedops.ObserverPayloadType,
		Payload:     base64.StdEncoding.EncodeToString(payload),
		Signatures: []verifiedops.ObserverSignature{{
			KeyID: "observer-key",
			Sig:   base64.StdEncoding.EncodeToString(ed25519.Sign(priv, pae)),
		}},
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return raw, verifiedops.ObserverTrust{KeyID: "observer-key", PublicKey: pub}
}

func TestObserverNonceDurableReplayFenceWithOriginalACK(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	want := verifiedops.ObserverSubject{
		OperationID: binding.OperationID, ClusterID: string(binding.ClusterID),
		ProjectID: binding.ProjectID, WorkloadID: string(binding.WorkloadID),
		Generation: binding.Generation, JobUID: binding.JobUID,
		Nonce:      strings.Repeat("a", 64),
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, trust := signedNonceFixture(t, want)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, want); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("unsigned/unbound original CREATE was accepted: %v", err)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, want); err != nil {
		t.Fatalf("first valid signed proof denied: %v", err)
	}
	if err := New(store.db).RecordSignedObserverNonce(ctx, raw, trust, want); err != nil {
		t.Fatalf("idempotent retry after process restart failed: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `
SELECT count(*) FROM verified_observer_nonce_receipts
WHERE cluster_id=$1 AND nonce=$2
`, binding.ClusterID, want.Nonce).Scan(&count); err != nil || count != 1 {
		t.Fatalf("nonce replay inserted duplicate: count=%d err=%v", count, err)
	}
	changed := want
	changed.ObservedAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	changedRaw, changedTrust := signedNonceFixture(t, changed)
	if err := store.RecordSignedObserverNonce(ctx, changedRaw, changedTrust, changed); !errors.Is(err, ErrObserverEvidenceReplayed) {
		t.Fatalf("different signed proof reused nonce: %v", err)
	}
	mismatch := want
	mismatch.JobUID = "another-uid"
	mismatchRaw, mismatchTrust := signedNonceFixture(t, mismatch)
	if err := store.RecordSignedObserverNonce(ctx, mismatchRaw, mismatchTrust, mismatch); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("same-name replacement claimed original Job: %v", err)
	}
	if err := store.RecordSignedObserverNonce(ctx, raw, verifiedops.ObserverTrust{KeyID: "wrong", PublicKey: trust.PublicKey}, want); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("incorrect verifier root accepted: %v", err)
	}
}

func TestObserverNonceScopeFreshnessAndSignatureFailClosed(t *testing.T) {
	store, _ := newCreateUIDFixture(t)
	ctx := context.Background()
	base := verifiedops.ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "cluster",
		ProjectID: "project", WorkloadID: "workload", Generation: 1,
		JobUID: "uid", Nonce: strings.Repeat("b", 64),
		ObservedAt: time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	raw, trust := signedNonceFixture(t, base)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, base); !errors.Is(err, ErrObserverEvidenceStale) {
		t.Fatalf("stale evidence accepted: %v", err)
	}
	base.ObservedAt = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	raw, trust = signedNonceFixture(t, base)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, base); !errors.Is(err, ErrObserverEvidenceStale) {
		t.Fatalf("future evidence accepted: %v", err)
	}
	base.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	base.Nonce = "predictable"
	raw, trust = signedNonceFixture(t, base)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, base); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("malformed nonce accepted: %v", err)
	}
	base.Nonce = strings.Repeat("b", 64)
	raw, trust = signedNonceFixture(t, base)
	tampered := append([]byte(nil), raw...)
	if len(tampered) > 5 {
		tampered[len(tampered)-4] ^= 1
	}
	if err := store.RecordSignedObserverNonce(ctx, tampered, trust, base); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("modified signed envelope accepted: %v", err)
	}
}
