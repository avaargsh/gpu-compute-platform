package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
)

type observerEvidenceRunnerFunc func(context.Context, []byte) ([]byte, error)

func (f observerEvidenceRunnerFunc) RunObserver(ctx context.Context, ticket []byte) ([]byte, error) {
	return f(ctx, ticket)
}

func TestDispatchAndRecordSignedObservedJobEndToEnd(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	dispatchPub, dispatchPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: input.JobName, Namespace: input.Namespace, UID: types.UID(input.JobUID),
			ResourceVersion: "ack-rv",
			Labels: map[string]string{"ai.compute/workload": input.JobName},
			Annotations: map[string]string{"ai.compute/generation": "1"},
		},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "workload", Image: "registry.invalid/train@" + trustedTicketImage,
			}},
		}}},
	}
	var runnerCalled bool
	runner := observerEvidenceRunnerFunc(func(ctx context.Context, ticket []byte) ([]byte, error) {
		runnerCalled = true
		approved, err := verifiedops.ApproveObserverWorkTicket(ticket,
			verifiedops.ObserverTrust{KeyID: "dispatcher", PublicKey: dispatchPub})
		if err != nil {
			return nil, err
		}
		envelope, observation, err := verifiedops.RunApprovedObserverWork(ctx,
			fake.NewSimpleClientset(job), approved,
			verifiedops.ObserverSigner{KeyID: "observer", Signer: observerPriv})
		if err != nil {
			return nil, err
		}
		if observation.Code != verifiedops.OutcomePending {
			t.Fatalf("expected non-positive pending observation, got %#v", observation)
		}
		return envelope, nil
	})

	receipt, err := store.DispatchAndRecordSignedObservedJob(
		context.Background(), makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatchPriv},
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub}, runner)
	if err != nil {
		t.Fatalf("dispatch -> signed observation -> durable receipt: %v", err)
	}
	if !runnerCalled {
		t.Fatal("isolated Observer runner was not called")
	}
	if receipt.OperationID != input.OperationID || receipt.Nonce == "" ||
		receipt.Observation.Code != verifiedops.OutcomePending || receipt.EnvelopeSHA256 == "" {
		t.Fatalf("unexpected durable observation receipt: %#v", receipt)
	}
	if receipt.Observation.Code == "AUTHORIZED_AND_VERIFIED" {
		t.Fatal("observation must never become a final authorization verdict")
	}
}

func TestDispatchAndRecordRejectsMissingRunnerBeforeIssuingChallenge(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	_, dispatcherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, observerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPriv.Public().(ed25519.PublicKey)},
		nil)
	if !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("missing runner should fail closed, got %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_challenges`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("missing runner minted %d challenges before failing closed", count)
	}
}

func TestDispatchAndRecordRejectsMalformedEvidenceWithoutReceipt(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	_, dispatcherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	runner := observerEvidenceRunnerFunc(func(context.Context, []byte) ([]byte, error) {
		return []byte(`{"payload":"not-a-valid-dsse-envelope"}`), nil
	})
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub}, runner)
	if err == nil {
		t.Fatal("malformed evidence was accepted")
	}
	var count int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_nonce_receipts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("malformed evidence created %d receipts", count)
	}
	_ = observerPriv // key exists to emphasize that trust configuration alone cannot validate unsigned output.
}
