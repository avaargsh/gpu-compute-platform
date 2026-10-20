package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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

const trustedTicketImage = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func prepareTrustedObserverIssue(t *testing.T) (*Store, JobCreateUIDBinding) {
	t.Helper()
	store, input := newCreateUIDFixture(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `
UPDATE desired_resources
SET spec=jsonb_set(spec,'{image}',to_jsonb($3::text),true)
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, input.ClusterID, input.WorkloadID, "registry.invalid/train@"+trustedTicketImage); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareJobCreateAttempt(ctx, durableAttempt(input)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(ctx, input); err != nil {
		t.Fatal(err)
	}
	return store, input
}

func TestSignedDispatchTicketIsDerivedFromDurableOperationAndVerifies(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueSignedObserverWork(context.Background(),
		makeObserverScope(input), verifiedops.ObserverSigner{KeyID: "trusted-dispatcher", Signer: priv})
	if err != nil {
		t.Fatal(err)
	}
	if issued.Expected.Namespace != input.Namespace || issued.Expected.Name != input.JobName ||
		string(issued.Expected.JobUID) != input.JobUID || issued.Expected.ImageDigest != trustedTicketImage ||
		issued.Challenge.OperationID != input.OperationID || issued.Challenge.Nonce == "" {
		t.Fatalf("ticket did not derive identity from original CREATE: %#v", issued)
	}
	if !issued.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("ticket already expired at issuance: %#v", issued)
	}
	if _, err := verifiedops.ApproveObserverWorkTicket(issued.Ticket,
		verifiedops.ObserverTrust{KeyID: "trusted-dispatcher", PublicKey: pub}); err != nil {
		t.Fatalf("pinned dispatcher root rejected valid ticket: %v", err)
	}
	if _, err := verifiedops.ApproveObserverWorkTicket(issued.Ticket,
		verifiedops.ObserverTrust{KeyID: "other-dispatcher", PublicKey: pub}); !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("foreign dispatcher root accepted: %v", err)
	}

	var env verifiedops.SignedObserverEnvelope
	if err := json.Unmarshal(issued.Ticket, &env); err != nil {
		t.Fatal(err)
	}
	body, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var ticket verifiedops.ObserverWorkTicket
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket.Expected.ImageDigest != trustedTicketImage ||
		ticket.Expected.Name != input.JobName ||
		ticket.Challenge.Nonce != issued.Challenge.Nonce {
		t.Fatalf("signed body not bound to saved Job: %#v", ticket)
	}
	start, err := time.Parse(time.RFC3339Nano, ticket.IssuedAt)
	if err != nil {
		t.Fatal(err)
	}
	end, err := time.Parse(time.RFC3339Nano, ticket.ExpiresAt)
	if err != nil || !end.After(start) || end.Sub(start) > 2*time.Minute {
		t.Fatalf("server challenge overran ticket max TTL: %v %v %v", start, end, err)
	}
}

func TestTicketIssuanceRejectsUnprovenCreateAndChangedContext(t *testing.T) {
	store, input := newCreateUIDFixture(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := verifiedops.ObserverSigner{KeyID: "trusted-dispatcher", Signer: private}
	scope := makeObserverScope(input)
	if _, err := store.IssueSignedObserverWork(context.Background(), scope, dispatch); !errors.Is(err, ErrObserverNonceUnproven) {
		t.Fatalf("issued ticket without PREPARED+ACK: %v", err)
	}
	if _, err := store.db.ExecContext(context.Background(), `
UPDATE desired_resources
SET spec=jsonb_set(spec,'{image}',to_jsonb($3::text),true)
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, input.ClusterID, input.WorkloadID, "registry.invalid/train@"+trustedTicketImage); err != nil {
		t.Fatal(err)
	}
	if err := store.PrepareJobCreateAttempt(context.Background(), durableAttempt(input)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordJobCreateUID(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ObserverChallengeScope)
	}{
		{"wrong-project", func(s *ObserverChallengeScope) { s.ProjectID = "foreign" }},
		{"wrong-uid", func(s *ObserverChallengeScope) { s.JobUID = "replaced-by-name" }},
		{"wrong-operation", func(s *ObserverChallengeScope) { s.OperationID = strings.Repeat("f", 32) }},
		{"wrong-generation", func(s *ObserverChallengeScope) { s.Generation++ }},
		{"wrong-workload", func(s *ObserverChallengeScope) { s.WorkloadID = "another" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := scope
			test.mutate(&other)
			if _, err := store.IssueSignedObserverWork(context.Background(), other, dispatch); !errors.Is(err, ErrObserverNonceUnproven) {
				t.Fatalf("unsafe scope issued signed work: %v", err)
			}
		})
	}
	var count int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_challenges`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid ticket issuance minted a challenge: count=%d err=%v", count, err)
	}
}

func TestTicketIssuanceRejectsUnpinnedImageDeletedDesiredOrMissingSigner(t *testing.T) {
	for _, fail := range []string{"image", "deleted", "signer"} {
		t.Run(fail, func(t *testing.T) {
			store, input := prepareTrustedObserverIssue(t)
			_, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer := verifiedops.ObserverSigner{KeyID: "trusted", Signer: private}
			switch fail {
			case "image":
				_, err = store.db.ExecContext(context.Background(), `
UPDATE desired_resources SET spec=jsonb_set(spec,'{image}','"image:latest"'::jsonb,true)
WHERE cluster_id=$1 AND kind='Workload' AND resource_id=$2
`, input.ClusterID, input.WorkloadID)
			case "deleted":
				err = store.MarkDesiredDeleting(context.Background(), input.ClusterID,
					"Workload", input.WorkloadID, time.Now().UTC())
			case "signer":
				signer.KeyID = ""
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.IssueSignedObserverWork(context.Background(), makeObserverScope(input), signer)
			want := ErrObserverNonceUnproven
			if fail == "signer" {
				want = verifiedops.ErrObserverUntrusted
			}
			if !errors.Is(err, want) {
				t.Fatalf("ticket issued for %s: %v", fail, err)
			}
			var count int
			if err := store.db.QueryRowContext(context.Background(),
				`SELECT count(*) FROM verified_observer_challenges`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("%s failure left challenge: %d %v", fail, count, err)
			}
		})
	}
}

func TestIssuedSignedTicketCanReachPendingV2ReceiptWithoutAuthorityVerdict(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	dispatchPublic, dispatchPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	issued, err := store.IssueSignedObserverWork(context.Background(), makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatchPrivate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedops.ApproveObserverWorkTicket(issued.Ticket,
		verifiedops.ObserverTrust{KeyID: "dispatcher", PublicKey: dispatchPublic}); err != nil {
		t.Fatal(err)
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: input.JobName, Namespace: input.Namespace, UID: types.UID(input.JobUID),
			ResourceVersion: "ack-rv", Labels: map[string]string{"ai.compute/workload": input.JobName},
			Annotations: map[string]string{"ai.compute/generation": "1"},
		},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "workload", Image: "registry.invalid/train@" + trustedTicketImage}},
		}}},
	}
	observerPublic, observerPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, observation, err := verifiedops.NewJobObserver(fake.NewSimpleClientset(job)).ObserveAndSign(
		context.Background(), issued.Challenge, issued.Expected,
		verifiedops.ObserverSigner{KeyID: "observer", Signer: observerPrivate})
	if err != nil || observation.Code != verifiedops.OutcomePending {
		t.Fatalf("independent pending proof: %#v %v", observation, err)
	}
	receipt, err := store.RecordSignedObservedJobProof(context.Background(), raw,
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPublic},
		makeObserverScope(input), issued.Challenge.Nonce)
	if err != nil || receipt.Observation.Code != verifiedops.OutcomePending {
		t.Fatalf("signed ticket -> v2 receipt: %#v %v", receipt, err)
	}
	if receipt.Observation.Code == "AUTHORIZED_AND_VERIFIED" {
		t.Fatal("no final authorization verdict is permitted")
	}
}
