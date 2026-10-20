package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
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
			Labels:          map[string]string{"ai.compute/workload": input.JobName},
			Annotations:     map[string]string{"ai.compute/generation": "1"},
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
		client := fake.NewSimpleClientset(job)
		client.PrependReactor("create", "selfsubjectreviews", func(_ ktesting.Action) (bool, runtime.Object, error) {
			return true, &authenticationv1.SelfSubjectReview{
				Status: authenticationv1.SelfSubjectReviewStatus{
					UserInfo: authenticationv1.UserInfo{Username: verifiedops.ObserverIdentityUsername},
				},
			}, nil
		})
		envelope, observation, err := verifiedops.RunApprovedObserverWork(ctx,
			client, approved, verifiedops.ObserverSigner{KeyID: "observer", Signer: observerPriv})
		if err != nil {
			return nil, err
		}
		if observation.Code != verifiedops.OutcomePending {
			t.Fatalf("expected non-positive pending observation, got %#v", observation)
		}
		return envelope, nil
	})

	registerObserverTrustForTest(t, store, input.ClusterID,
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub})
	receipt, err := store.DispatchAndRecordSignedObservedJob(
		context.Background(), makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatchPriv},
		"observer", runner)
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
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		"observer", nil)
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
	observerPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	runner := observerEvidenceRunnerFunc(func(context.Context, []byte) ([]byte, error) {
		return []byte(`{"payload":"not-a-valid-dsse-envelope"}`), nil
	})
	registerObserverTrustForTest(t, store, input.ClusterID,
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub})
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		"observer", runner)
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
}

func TestDispatchAndRecordDeliveryFailureLeavesNoReceipt(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	_, dispatcherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	deliveryErr := errors.New("observer delivery timed out")
	runner := observerEvidenceRunnerFunc(func(context.Context, []byte) ([]byte, error) {
		return nil, deliveryErr
	})

	registerObserverTrustForTest(t, store, input.ClusterID,
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub})
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		"observer", runner)
	if !errors.Is(err, deliveryErr) {
		t.Fatalf("delivery failure should propagate, got %v", err)
	}

	var challenges, receipts int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_challenges`).Scan(&challenges); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_nonce_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if challenges != 1 {
		t.Fatalf("expected one short-lived challenge for failed delivery, got %d", challenges)
	}
	if receipts != 0 {
		t.Fatalf("failed delivery must not persist an observation receipt, got %d", receipts)
	}
}

func TestDispatchAndRecordRejectsOversizedEvidenceWithoutReceipt(t *testing.T) {
	store, input := prepareTrustedObserverIssue(t)
	_, dispatcherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registerObserverTrustForTest(t, store, input.ClusterID,
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub})

	runner := observerEvidenceRunnerFunc(func(context.Context, []byte) ([]byte, error) {
		return make([]byte, maxObserverEvidenceEnvelopeBytes+1), nil
	})
	_, err = store.DispatchAndRecordSignedObservedJob(context.Background(),
		makeObserverScope(input),
		verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcherPriv},
		"observer", runner)
	if !errors.Is(err, verifiedops.ErrObserverUntrusted) {
		t.Fatalf("oversized evidence must fail closed, got %v", err)
	}

	var challenges, receipts int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_challenges`).Scan(&challenges); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM verified_observer_nonce_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if challenges != 1 {
		t.Fatalf("expected one short-lived challenge after delivery, got %d", challenges)
	}
	if receipts != 0 {
		t.Fatalf("oversized evidence created %d receipts", receipts)
	}
}
