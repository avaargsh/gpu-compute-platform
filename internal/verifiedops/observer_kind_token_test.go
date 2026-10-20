package verifiedops

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const observerProjectA = "verifiedops-token-a"
const observerProjectB = "verifiedops-token-b"

// Real API-server acceptance with a Kubernetes-issued, short-lived token.
// Unlike kubectl --as, this client has NO admin x509 credentials or
// impersonation options; it can exercise only its own ServiceAccount RBAC.
// The CI job prepares the standalone SA and a single reviewed project Role.
func TestRealKindObserverShortLivedSATokenAndBoundJob(t *testing.T) {
	if os.Getenv("VERIFIEDOPS_OBSERVER_TOKEN_E2E") != "1" {
		t.Skip("requires real kind API server and verifiedops-observer RBAC")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	adminCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := kubernetes.NewForConfig(adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	ttl := int64(600)
	token, err := admin.CoreV1().ServiceAccounts("verifiedops-observer").CreateToken(
		ctx, "verifiedops-observer", &authnv1.TokenRequest{
			Spec: authnv1.TokenRequestSpec{ExpirationSeconds: &ttl},
		}, metav1.CreateOptions{},
	)
	if err != nil || token == nil || token.Status.Token == "" {
		t.Fatalf("Kubernetes API server did not issue a bounded ServiceAccount token: %v", err)
	}
	if token.Status.ExpirationTimestamp.IsZero() ||
		token.Status.ExpirationTimestamp.Time.After(time.Now().Add(11*time.Minute)) {
		t.Fatal("token expiration not bounded to ten minutes")
	}

	// Build a *fresh* REST client with only CA/server and the issued token.
	// Do not copy admin configuration; it carries kind's client certificate.
	observerCfg := &rest.Config{
		Host:        adminCfg.Host,
		BearerToken: token.Status.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:     adminCfg.TLSClientConfig.CAData,
			CAFile:     adminCfg.TLSClientConfig.CAFile,
			ServerName: adminCfg.TLSClientConfig.ServerName,
			Insecure:   adminCfg.TLSClientConfig.Insecure,
		},
	}
	reader, err := kubernetes.NewForConfig(observerCfg)
	if err != nil {
		t.Fatal(err)
	}
	self, err := reader.AuthenticationV1().SelfSubjectReviews().Create(
		ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if self.Status.UserInfo.Username != "system:serviceaccount:verifiedops-observer:verifiedops-observer" {
		t.Fatalf("observer token authenticated as unexpected subject %q", self.Status.UserInfo.Username)
	}

	// The admin supplies a *real* CREATE response UID. No same-name GET may
	// be turned into an operation-owned CREATE receipt.
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	job, err := admin.BatchV1().Jobs(observerProjectA).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "job-token-observer",
			Labels:      map[string]string{"ai.compute/workload": "job-token-observer"},
			Annotations: map[string]string{"ai.compute/generation": "1"},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name: "workload", Image: "example.invalid/blocked@" + digest,
				}},
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.UID == "" || job.ResourceVersion == "" {
		t.Fatalf("API server did not return real Job UID/RV: %#v", job)
	}
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = admin.BatchV1().Jobs(observerProjectA).Delete(cleanupCtx, job.Name, metav1.DeleteOptions{})
	}()
	binding := ExpectedJob{
		Namespace: observerProjectA, Name: job.Name, JobUID: job.UID,
		Generation: 1, ContainerName: "workload", ImageDigest: digest,
	}
	view, err := NewJobObserver(reader).Observe(ctx, binding)
	if err != nil || view.Code != OutcomePending || view.JobUID != job.UID {
		t.Fatalf("actual token could not observe bound but pending Job: %#v err %v", view, err)
	}
	// Proof generation uses the short-lived SA bearer client, not kind
	// admin credentials. The test key is ephemeral and never deployed.
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	challenge := ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "real-kind",
		ProjectID: observerProjectA, WorkloadID: job.Name,
		Generation: 1, JobUID: string(job.UID), Nonce: strings.Repeat("b", 64),
	}
	signed, signedState, err := NewJobObserver(reader).ObserveAndSign(ctx, challenge, binding,
		ObserverSigner{KeyID: "kind-ephemeral-only", Signer: private})
	if err != nil || signedState.Code != OutcomePending {
		t.Fatalf("real SA sign pending evidence: %#v %v", signedState, err)
	}
	evidence, err := VerifyObservedJobEnvelope(signed,
		ObserverTrust{KeyID: "kind-ephemeral-only", PublicKey: pub}, challenge, binding)
	if err != nil || evidence.Observation.Code != OutcomePending || evidence.Subject.JobUID != string(job.UID) {
		t.Fatalf("pending real-API evidence was misverified: %#v %v", evidence, err)
	}
	// Real TokenRequest-only Kubernetes client must run under an independently
	// pinned dispatcher ticket before it can create an Observer signature.
	dispatcherPub, dispatcherKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	workTicket := ObserverWorkTicket{
		Challenge: challenge,
		Expected:  binding,
		IssuedAt:  now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	request, err := AuthorObserverWorkTicket(workTicket, ObserverSigner{
		KeyID: "dispatcher-test-only", Signer: dispatcherKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ApproveObserverWorkTicket(request, ObserverTrust{
		KeyID: "dispatcher-test-only", PublicKey: dispatcherPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	workerProof, workerState, err := RunApprovedObserverWork(ctx, reader, approved,
		ObserverSigner{KeyID: "kind-ephemeral-only", Signer: private})
	if err != nil || workerState.Code != OutcomePending {
		t.Fatalf("real bearer-only signed work-ticket observation: %#v err %v", workerState, err)
	}
	if _, err := VerifyObservedJobEnvelope(workerProof,
		ObserverTrust{KeyID: "kind-ephemeral-only", PublicKey: pub}, challenge, binding); err != nil {
		t.Fatalf("real bearer-only ticket proof was not verifiable: %v", err)
	}
	stale := binding
	stale.JobUID = types.UID("old-replaced-job-uid")
	view, err = NewJobObserver(reader).Observe(ctx, stale)
	if err != nil || view.Code != OwnershipUnproven {
		t.Fatalf("replaced bound UID was accepted by actual SA: %#v err %v", view, err)
	}
	if view.Code == "AUTHORIZED_AND_VERIFIED" {
		t.Fatal("an unsigned observation cannot become an authorized verdict")
	}

	// The test must use real bearer credentials, NOT kubectl admin --as.
	if _, err := reader.CoreV1().Pods(observerProjectA).List(ctx, metav1.ListOptions{}); err != nil {
		t.Fatalf("allowed project Pod LIST: %v", err)
	}
	nodes, err := admin.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		t.Fatalf("kind Node discovery: %v", err)
	}
	if _, err := reader.CoreV1().Nodes().Get(ctx, nodes.Items[0].Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("allowed one Node GET: %v", err)
	}
	for name, probe := range map[string]func() error{
		"project-b-job": func() error {
			_, err := reader.BatchV1().Jobs(observerProjectB).Get(ctx, job.Name, metav1.GetOptions{})
			return err
		},
		"project-b-pods": func() error {
			_, err := reader.CoreV1().Pods(observerProjectB).List(ctx, metav1.ListOptions{})
			return err
		},
		"secret-list": func() error {
			_, err := reader.CoreV1().Secrets(observerProjectA).List(ctx, metav1.ListOptions{})
			return err
		},
		"node-list": func() error {
			_, err := reader.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			return err
		},
		"job-list": func() error {
			_, err := reader.BatchV1().Jobs(observerProjectA).List(ctx, metav1.ListOptions{})
			return err
		},
		"job-delete": func() error {
			return reader.BatchV1().Jobs(observerProjectA).Delete(ctx, job.Name, metav1.DeleteOptions{})
		},
		"pod-create": func() error {
			_, err := reader.CoreV1().Pods(observerProjectA).Create(ctx,
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "forbidden-writer"}},
				metav1.CreateOptions{})
			return err
		},
	} {
		if err := probe(); !apierrors.IsForbidden(err) {
			t.Errorf("%s must return real Kubernetes Forbidden, got %v", name, err)
		}
	}
	after, err := admin.BatchV1().Jobs(observerProjectA).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil || after.UID != job.UID {
		t.Fatalf("forbidden delete changed actual Job: %s, err=%v", fmt.Sprint(after), err)
	}

	// Exercise actual API-server deletion semantics using the independently
	// authenticated Observer bearer token. A dedicated finalizer retains the
	// deleting Job long enough for the Observer to GET DeletionTimestamp.
	// The Observer does not PATCH/DELETE: only the privileged test setup does.
	const deletionFenceFinalizer = "verifiedops.gpupanel.dev/e2e-deletion-fence"
	_, err = admin.BatchV1().Jobs(observerProjectA).Patch(ctx, job.Name,
		types.MergePatchType,
		[]byte(`{"metadata":{"finalizers":["verifiedops.gpupanel.dev/e2e-deletion-fence"]}}`),
		metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("cannot retain real Job for deletion test: %v", err)
	}
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		// Never leave a test Job stuck terminating, including on t.Fatal.
		_, cleanupErr := admin.BatchV1().Jobs(observerProjectA).Patch(cleanupCtx, job.Name,
			types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{})
		if cleanupErr != nil && !apierrors.IsNotFound(cleanupErr) {
			t.Logf("remove e2e deletion finalizer: %v", cleanupErr)
		}
	}()
	if err := admin.BatchV1().Jobs(observerProjectA).Delete(ctx, job.Name,
		metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &job.UID}}); err != nil {
		t.Fatalf("API server rejected UID-fenced DELETE: %v", err)
	}
	deleting, err := reader.BatchV1().Jobs(observerProjectA).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil || deleting.UID != job.UID || deleting.ResourceVersion == "" ||
		deleting.DeletionTimestamp == nil {
		t.Fatalf("real Observer token cannot read API-server deletion: %#v %v", deleting, err)
	}
	foundFinalizer := false
	for _, finalizer := range deleting.Finalizers {
		if finalizer == deletionFenceFinalizer {
			foundFinalizer = true
		}
	}
	if !foundFinalizer {
		t.Fatalf("deleting Job lost the test retention finalizer: %#v", deleting.Finalizers)
	}
	negative, err := NewJobObserver(reader).Observe(ctx, binding)
	if err != nil || negative.Code != OutcomePending || negative.Code == PODReadyObserved {
		t.Fatalf("deleting real Job became Ready: %#v %v", negative, err)
	}
	// Use a fresh test-only dispatcher ticket; no external dispatcher,
	// production signer or PostgreSQL issuance is inferred by this test.
	deletedChallenge := challenge
	deletedChallenge.Nonce = strings.Repeat("c", 64)
	issued := time.Now().UTC()
	deletedTicket := ObserverWorkTicket{
		Challenge: deletedChallenge, Expected: binding,
		IssuedAt: issued.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: issued.Add(time.Minute).Format(time.RFC3339Nano),
	}
	deletedRequest, err := AuthorObserverWorkTicket(deletedTicket, ObserverSigner{
		KeyID: "dispatcher-test-only", Signer: dispatcherKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvedDeleted, err := ApproveObserverWorkTicket(deletedRequest, ObserverTrust{
		KeyID: "dispatcher-test-only", PublicKey: dispatcherPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	deletedProof, deletedState, err := RunApprovedObserverWork(ctx, reader, approvedDeleted,
		ObserverSigner{KeyID: "kind-ephemeral-only", Signer: private})
	if err != nil || deletedState.Code != OutcomePending {
		t.Fatalf("deletion emitted positive or unsigned evidence: %#v %v", deletedState, err)
	}
	verifiedDeleted, err := VerifyObservedJobEnvelope(deletedProof,
		ObserverTrust{KeyID: "kind-ephemeral-only", PublicKey: pub}, deletedChallenge, binding)
	if err != nil || verifiedDeleted.Observation.Code != OutcomePending ||
		verifiedDeleted.Observation.Code == PODReadyObserved {
		t.Fatalf("deletion evidence was promoted to Ready: %#v %v", verifiedDeleted, err)
	}
	// This exercises the FIRST deletion-aware GET on a real API server.
	// The second-GET transition remains covered deterministically by the
	// race tests added in #163 and #164, not by this cluster test.
}
