package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
)

const v2Digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func registerObserverTrustForTest(t *testing.T, store *Store, clusterID interface{}, trust verifiedops.ObserverTrust) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), `
INSERT INTO verified_observer_signing_keys (cluster_id,key_id,public_key,valid_from)
VALUES ($1,$2,$3,clock_timestamp()-interval '1 minute')
`, clusterID, trust.KeyID, []byte(trust.PublicKey)); err != nil {
		t.Fatal(err)
	}
}

func pendingV2Proof(t *testing.T) (*Store, JobCreateUIDBinding, ObserverChallenge, []byte, verifiedops.ObserverTrust) {
	t.Helper()
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `
UPDATE desired_resources SET spec=jsonb_set(spec,'{image}',to_jsonb($3::text),true)
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, binding.ClusterID, binding.WorkloadID, "registry.invalid/train@"+v2Digest); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, binding); err != nil {
		t.Fatal(err)
	}
	challenge, err := store.IssueObserverChallenge(ctx, makeObserverScope(binding))
	if err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: binding.JobName, Namespace: binding.Namespace, UID: types.UID(binding.JobUID),
			ResourceVersion: "180",
			Annotations:     map[string]string{"ai.compute/generation": "1"},
			Labels:          map[string]string{"ai.compute/workload": binding.JobName},
		},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "workload", Image: "registry.invalid/train@" + v2Digest}},
		}}},
	}
	subject := verifiedops.ObserverSubject{
		OperationID: challenge.OperationID, ClusterID: string(challenge.ClusterID),
		ProjectID: challenge.ProjectID, WorkloadID: string(challenge.WorkloadID),
		Generation: challenge.Generation, JobUID: challenge.JobUID, Nonce: challenge.Nonce,
	}
	expected := verifiedops.ExpectedJob{
		Namespace: binding.Namespace, Name: binding.JobName, JobUID: types.UID(binding.JobUID),
		Generation: binding.Generation, ContainerName: "workload", ImageDigest: v2Digest,
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registerObserverTrustForTest(t, store, binding.ClusterID, verifiedops.ObserverTrust{KeyID: "pinned-test-only", PublicKey: pub})
	raw, observed, err := verifiedops.NewJobObserver(fake.NewSimpleClientset(job)).ObserveAndSign(
		ctx, subject, expected, verifiedops.ObserverSigner{KeyID: "pinned-test-only", Signer: priv})
	if err != nil || observed.Code != verifiedops.OutcomePending {
		t.Fatalf("signed pending fixture: %#v %v", observed, err)
	}
	return store, binding, challenge, raw, verifiedops.ObserverTrust{KeyID: "pinned-test-only", PublicKey: pub}
}

func TestSignedObservedJobV2DurablePendingAndCrossVersionReplay(t *testing.T) {
	store, binding, challenge, raw, trust := pendingV2Proof(t)
	ctx := context.Background()
	scope := makeObserverScope(binding)
	receipt, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, scope, challenge.Nonce)
	if err != nil || receipt.Observation.Code != verifiedops.OutcomePending {
		t.Fatalf("v2 evidence not accepted as pending: %#v %v", receipt, err)
	}
	if receipt.Observation.Code == "AUTHORIZED_AND_VERIFIED" {
		t.Fatal("receipt falsely authorized execution")
	}
	retry, err := New(store.db).RecordSignedObservedJobProof(ctx, raw, trust.KeyID, scope, challenge.Nonce)
	if err != nil || retry.EnvelopeSHA256 != receipt.EnvelopeSHA256 {
		t.Fatalf("durable retry not idempotent: %#v %v", retry, err)
	}
	var count int
	var version, code string
	err = store.db.QueryRowContext(ctx, `
SELECT count(*),max(predicate_type),max(observation_json->>'Code')
FROM verified_observer_nonce_receipts WHERE cluster_id=$1 AND nonce=$2
`, binding.ClusterID, challenge.Nonce).Scan(&count, &version, &code)
	if err != nil || count != 1 || version != verifiedops.ObservedJobPredicateType || code != verifiedops.OutcomePending {
		t.Fatalf("v2 receipt corrupted: count=%d version=%s code=%s err=%v", count, version, code, err)
	}
	legacySubject := subjectForChallenge(challenge)
	legacyRaw, legacyTrust := signedNonceFixture(t, legacySubject)
	if err := store.RecordSignedObserverNonce(ctx, legacyRaw, legacyTrust, legacySubject); !errors.Is(err, ErrObserverEvidenceReplayed) {
		t.Fatalf("v1 reused v2 nonce: %v", err)
	}
}

func TestSignedObservedJobV2RejectsForgedScopeAndUnissuedChallenge(t *testing.T) {
	store, binding, challenge, raw, trust := pendingV2Proof(t)
	ctx := context.Background()
	scope := makeObserverScope(binding)
	wrong := scope
	wrong.ProjectID = "other-project"
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, wrong, challenge.Nonce); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("wrong tenant accepted: %v", err)
	}
	wrong = scope
	wrong.JobUID = "replacement-uid"
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, wrong, challenge.Nonce); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("same-name new UID accepted: %v", err)
	}
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, scope, strings.Repeat("f", 64)); !errors.Is(err, ErrObserverChallengeMissing) {
		t.Fatalf("unissued nonce accepted: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM verified_observer_nonce_receipts").Scan(&count); err != nil || count != 0 {
		t.Fatalf("bad evidence consumed nonce: %d %v", count, err)
	}
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, scope, challenge.Nonce); err != nil {
		t.Fatalf("rejected attempts poisoned legitimate challenge: %v", err)
	}
}

func TestSignedObservedJobV2ExpiredDeletedAndChangedDesired(t *testing.T) {
	for _, mode := range []string{"expired", "deleted", "changed-image"} {
		t.Run(mode, func(t *testing.T) {
			store, binding, challenge, raw, trust := pendingV2Proof(t)
			ctx := context.Background()
			switch mode {
			case "expired":
				_, err := store.db.ExecContext(ctx, `
UPDATE verified_observer_challenges
SET issued_at=clock_timestamp()-interval '3 minutes',
    expires_at=clock_timestamp()-interval '1 second'
WHERE cluster_id=$1 AND nonce=$2
`, binding.ClusterID, challenge.Nonce)
				if err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := store.MarkDesiredDeleting(ctx, binding.ClusterID, "Workload", binding.WorkloadID, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			case "changed-image":
				_, err := store.db.ExecContext(ctx, `
UPDATE desired_resources SET spec=jsonb_set(spec,'{image}',to_jsonb($3::text),true)
WHERE cluster_id=$1 AND resource_id=$2
`, binding.ClusterID, binding.WorkloadID, "registry.invalid/other@sha256:"+strings.Repeat("b", 64))
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, makeObserverScope(binding), challenge.Nonce)
			var expected error = ErrObserverNonceUnproven
			if mode == "expired" {
				expected = ErrObserverChallengeExpired
			}
			if mode == "changed-image" {
				expected = verifiedops.ErrObserverUntrusted
			}
			if !errors.Is(err, expected) {
				t.Fatalf("invalid %s proof accepted: %v expected %v", mode, err, expected)
			}
			var count int
			if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM verified_observer_nonce_receipts").Scan(&count); err != nil || count != 0 {
				t.Fatalf("failure wrote receipt: %d %v", count, err)
			}
		})
	}
}

func TestSignedObservedJobV2FirstSeenV1CannotConsumeAgain(t *testing.T) {
	store, binding, challenge, raw, trust := pendingV2Proof(t)
	ctx := context.Background()
	legacySubject := subjectForChallenge(challenge)
	legacyRaw, legacyTrust := signedNonceFixture(t, legacySubject)
	if err := store.RecordSignedObserverNonce(ctx, legacyRaw, legacyTrust, legacySubject); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, trust.KeyID, makeObserverScope(binding), challenge.Nonce); !errors.Is(err, ErrObserverEvidenceReplayed) {
		t.Fatalf("v2 bypassed v1 receipt: %v", err)
	}
}

func TestSignedObservedJobV2WrongRootAndAlteredEnvelope(t *testing.T) {
	store, binding, challenge, raw, trust := pendingV2Proof(t)
	ctx := context.Background()
	invalidTrust := trust
	invalidTrust.KeyID = "another-observer-key"
	if _, err := store.RecordSignedObservedJobProof(ctx, raw, invalidTrust.KeyID, makeObserverScope(binding), challenge.Nonce); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("foreign signer accepted: %v", err)
	}
	altered := append([]byte(nil), raw...)
	altered[len(altered)-8] ^= 1
	if _, err := store.RecordSignedObservedJobProof(ctx, altered, trust.KeyID, makeObserverScope(binding), challenge.Nonce); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("altered envelope accepted: %v", err)
	}
}

func TestSignedObservedJobV2RequiresActiveClusterScopedTrustRoot(t *testing.T) {
	for _, mode := range []string{"unknown-key", "revoked", "expired", "not-yet-valid", "foreign-cluster"} {
		t.Run(mode, func(t *testing.T) {
			store, binding, challenge, raw, trust := pendingV2Proof(t)
			ctx := context.Background()
			scope := makeObserverScope(binding)
			keyID := trust.KeyID
			switch mode {
			case "unknown-key":
				keyID = "not-registered"
			case "revoked":
				_, err := store.db.ExecContext(ctx, `
UPDATE verified_observer_signing_keys SET revoked_at=clock_timestamp()
WHERE cluster_id=$1 AND key_id=$2
`, binding.ClusterID, keyID)
				if err != nil {
					t.Fatal(err)
				}
			case "expired":
				_, err := store.db.ExecContext(ctx, `
UPDATE verified_observer_signing_keys SET valid_until=clock_timestamp()-interval '1 second'
WHERE cluster_id=$1 AND key_id=$2
`, binding.ClusterID, keyID)
				if err != nil { t.Fatal(err) }
			case "not-yet-valid":
				_, err := store.db.ExecContext(ctx, `
UPDATE verified_observer_signing_keys SET valid_from=clock_timestamp()+interval '1 minute'
WHERE cluster_id=$1 AND key_id=$2
`, binding.ClusterID, keyID)
				if err != nil { t.Fatal(err) }
			case "foreign-cluster":
				scope.ClusterID = "foreign-cluster"
			}
			_, err := store.RecordSignedObservedJobProof(ctx, raw, keyID, scope, challenge.Nonce)
			if !errors.Is(err, verifiedops.ErrObserverUntrusted) {
				t.Fatalf("inactive or foreign trust root accepted: %v", err)
			}
			var count int
			if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM verified_observer_nonce_receipts").Scan(&count); err != nil || count != 0 {
				t.Fatalf("untrusted root consumed receipt: count=%d err=%v", count, err)
			}
		})
	}
}
