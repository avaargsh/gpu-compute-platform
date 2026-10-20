package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
)

func makeObserverScope(b JobCreateUIDBinding) ObserverChallengeScope {
	return ObserverChallengeScope{
		OperationID: b.OperationID,
		ClusterID:   b.ClusterID,
		ProjectID:   b.ProjectID,
		WorkloadID:  b.WorkloadID,
		Generation:  b.Generation,
		JobUID:      b.JobUID,
	}
}

func subjectForChallenge(ch ObserverChallenge) verifiedops.ObserverSubject {
	return verifiedops.ObserverSubject{
		OperationID: ch.OperationID, ClusterID: string(ch.ClusterID),
		ProjectID: ch.ProjectID, WorkloadID: string(ch.WorkloadID),
		Generation: ch.Generation, JobUID: ch.JobUID,
		Nonce: ch.Nonce, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TestObserverChallengeRequiresOriginalCreateACKAndExactScope(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
	scope := makeObserverScope(binding)

	if _, err := store.IssueObserverChallenge(ctx, scope); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("challenge issued without ACK-bound UID: %v", err)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(binding)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, binding); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ObserverChallengeScope)
	}{
		{"foreign-project", func(x *ObserverChallengeScope) { x.ProjectID = "foreign-project" }},
		{"wrong-generation", func(x *ObserverChallengeScope) { x.Generation++ }},
		{"wrong-uid", func(x *ObserverChallengeScope) { x.JobUID = "new-uid-same-name" }},
		{"other-workload", func(x *ObserverChallengeScope) { x.WorkloadID = "another-workload" }},
		{"other-operation", func(x *ObserverChallengeScope) { x.OperationID = strings.Repeat("e", 32) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := scope
			tc.change(&x)
			if _, err := store.IssueObserverChallenge(ctx, x); !errors.Is(err, ErrObserverNonceUnproven) {
				t.Fatalf("unbound challenge scope granted: %v", err)
			}
		})
	}
	challenge, err := store.IssueObserverChallenge(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !signedObserverNonce.MatchString(challenge.Nonce) ||
		!challenge.ExpiresAt.After(challenge.IssuedAt) ||
		challenge.ExpiresAt.Sub(challenge.IssuedAt) > 3*time.Minute {
		t.Fatalf("invalid server challenge lifetime or nonce: %#v", challenge)
	}
	reissued, err := New(store.db).IssueObserverChallenge(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Nonce == reissued.Nonce {
		t.Fatal("successive independently generated challenges collide")
	}
}

func TestObserverChallengeNeverAcceptsGuessedStaleOrPreIssuedNonce(t *testing.T) {
	store, binding := newCreateUIDFixture(t)
	ctx := context.Background()
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
	want := subjectForChallenge(challenge)
	guessed := want
	guessed.Nonce = strings.Repeat("f", 64)
	raw, trust := signedNonceFixture(t, guessed)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, guessed); !errors.Is(err, ErrObserverChallengeMissing) {
		t.Fatalf("never-issued nonce accepted: %v", err)
	}

	early := want
	early.ObservedAt = challenge.IssuedAt.Add(-40 * time.Second).UTC().Format(time.RFC3339Nano)
	raw, trust = signedNonceFixture(t, early)
	if err := store.RecordSignedObserverNonce(ctx, raw, trust, early); !errors.Is(err, ErrObserverChallengeMismatch) {
		t.Fatalf("pre-issued observation accepted: %v", err)
	}
	// Rejected evidence must not burn a legitimate challenge.
	validRaw, validTrust := signedNonceFixture(t, want)
	if err := store.RecordSignedObserverNonce(ctx, validRaw, validTrust, want); err != nil {
		t.Fatalf("valid response denied after failed attempts: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `
SELECT count(*) FROM verified_observer_nonce_receipts
WHERE cluster_id=$1 AND nonce=$2
`, binding.ClusterID, challenge.Nonce).Scan(&count); err != nil || count != 1 {
		t.Fatalf("signed challenge was not consumed exactly once: count=%d err=%v", count, err)
	}
	if err := store.RecordSignedObserverNonce(ctx, validRaw, validTrust, want); err != nil {
		t.Fatalf("identical retry should be acknowledged during TTL: %v", err)
	}

	second, err := store.IssueObserverChallenge(ctx, makeObserverScope(binding))
	if err != nil {
		t.Fatal(err)
	}
	expired := subjectForChallenge(second)
	expiredRaw, expiredTrust := signedNonceFixture(t, expired)
	if _, err := store.db.ExecContext(ctx, `
UPDATE verified_observer_challenges
SET issued_at=clock_timestamp()-interval '3 minutes',
    expires_at=clock_timestamp()-interval '1 second'
WHERE cluster_id=$1 AND nonce=$2
`, binding.ClusterID, second.Nonce); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSignedObserverNonce(ctx, expiredRaw, expiredTrust, expired); !errors.Is(err, ErrObserverChallengeExpired) {
		t.Fatalf("expired challenge accepted: %v", err)
	}
	if err := store.RecordSignedObserverNonce(ctx, validRaw, validTrust, want); err != nil {
		t.Fatalf("unexpired earlier challenge must be unaffected: %v", err)
	}
}
