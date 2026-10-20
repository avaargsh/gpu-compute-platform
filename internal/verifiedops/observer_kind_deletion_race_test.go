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
	batchtyped "k8s.io/client-go/kubernetes/typed/batch/v1"
	coretyped "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// secondGetDeleteClient is a TEST HARNESS, not a production Observer.
// Both Job GETs and the Node GET use an actual read-only ServiceAccount
// bearer token against the API server. Only the intermediate Pod readiness
// snapshot is synthetic; the harness administrator triggers the real DELETE
// immediately before the second Job GET. Do not interpret this as a real GPU
// Pod Ready acceptance or as authority delegated to the Observer.
type secondGetDeleteClient struct {
	kubernetes.Interface
	admin     kubernetes.Interface
	namespace string
	jobName   string
	jobUID    types.UID
	pod       *corev1.Pod
	jobReads  int
}

type secondGetBatch struct {
	batchtyped.BatchV1Interface
	race *secondGetDeleteClient
}

type secondGetJobs struct {
	batchtyped.JobInterface
	race      *secondGetDeleteClient
	namespace string
}

func (c *secondGetDeleteClient) BatchV1() batchtyped.BatchV1Interface {
	return &secondGetBatch{BatchV1Interface: c.Interface.BatchV1(), race: c}
}

func (b *secondGetBatch) Jobs(namespace string) batchtyped.JobInterface {
	return &secondGetJobs{JobInterface: b.BatchV1Interface.Jobs(namespace), race: b.race, namespace: namespace}
}

func (j *secondGetJobs) Get(ctx context.Context, name string, options metav1.GetOptions) (*batchv1.Job, error) {
	if j.namespace == j.race.namespace && name == j.race.jobName {
		j.race.jobReads++
		if j.race.jobReads == 2 {
			// Only the independent TEST ADMIN performs this write. The
			// underlying Observer's bearer token has no write privileges.
			if err := j.race.admin.BatchV1().Jobs(j.namespace).Delete(ctx, name,
				metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &j.race.jobUID}}); err != nil {
				return nil, err
			}
		}
	}
	return j.JobInterface.Get(ctx, name, options)
}

type secondGetCore struct {
	coretyped.CoreV1Interface
	race *secondGetDeleteClient
}

type secondGetPods struct {
	coretyped.PodInterface
	race      *secondGetDeleteClient
	namespace string
}

func (c *secondGetDeleteClient) CoreV1() coretyped.CoreV1Interface {
	return &secondGetCore{CoreV1Interface: c.Interface.CoreV1(), race: c}
}

func (c *secondGetCore) Pods(namespace string) coretyped.PodInterface {
	return &secondGetPods{PodInterface: c.CoreV1Interface.Pods(namespace), race: c.race, namespace: namespace}
}

func (p *secondGetPods) List(ctx context.Context, options metav1.ListOptions) (*corev1.PodList, error) {
	if p.namespace == p.race.namespace {
		expectedSelector := "batch.kubernetes.io/controller-uid=" + string(p.race.jobUID)
		if options.LabelSelector != expectedSelector {
			return nil, fmt.Errorf("Observer used unexpected Pod selector %q", options.LabelSelector)
		}
		return &corev1.PodList{Items: []corev1.Pod{*p.race.pod.DeepCopy()}}, nil
	}
	return p.PodInterface.List(ctx, options)
}

func (p *secondGetPods) Get(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Pod, error) {
	if p.namespace == p.race.namespace && name == p.race.pod.Name {
		return p.race.pod.DeepCopy(), nil
	}
	return p.PodInterface.Get(ctx, name, options)
}

// A genuine kind API server supplies both Job GETs, the UID-preconditioned
// deletion marker and Node readiness. The positive-looking Pod snapshot is
// controlled, in-memory test input ONLY to reach the final Job readback.
// Real projected-token Pod execution, real ImageID and PostgreSQL receipt
// admission are separate release gates; this test makes no production claim.
func TestRealKindObserverJobDeletionOnSecondGET(t *testing.T) {
	if os.Getenv("VERIFIEDOPS_OBSERVER_TOKEN_E2E") != "1" {
		t.Skip("requires kind API server and the reviewed Observer RBAC fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	adminCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := kubernetes.NewForConfig(adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	ttl := int64(600)
	token, err := admin.CoreV1().ServiceAccounts("verifiedops-observer-e2e").CreateToken(
		ctx, "verifiedops-observer", &authnv1.TokenRequest{
			Spec: authnv1.TokenRequestSpec{ExpirationSeconds: &ttl},
		}, metav1.CreateOptions{})
	if err != nil || token == nil || token.Status.Token == "" {
		t.Fatalf("real short-lived Observer SA token missing: %v", err)
	}
	reader, err := kubernetes.NewForConfig(&rest.Config{
		Host:        adminCfg.Host,
		BearerToken: token.Status.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:     adminCfg.TLSClientConfig.CAData,
			CAFile:     adminCfg.TLSClientConfig.CAFile,
			ServerName: adminCfg.TLSClientConfig.ServerName,
			Insecure:   adminCfg.TLSClientConfig.Insecure,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := reader.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	// RBAC grants Node GET but intentionally denies Node LIST. Discover the
	// name via test admin, then independently GET using the bearer token.
	if err == nil {
		t.Fatal("Observer bearer token unexpectedly allowed cluster-wide Node LIST")
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("expected Node LIST denial, got %v", err)
	}
	nodes, err = admin.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var nodeName string
	for _, node := range nodes.Items {
		observedNode, getErr := reader.CoreV1().Nodes().Get(ctx, node.Name, metav1.GetOptions{})
		if getErr != nil {
			t.Fatal(getErr)
		}
		if readyNode(observedNode) && observedNode.UID != "" && observedNode.ResourceVersion != "" {
			nodeName = observedNode.Name
			break
		}
	}
	if nodeName == "" {
		t.Fatal("no real Ready kind Node available for the second-GET test")
	}

	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const name = "job-second-get-delete"
	const finalizer = "verifiedops.gpupanel.dev/e2e-second-get-deletion"
	suspended := true
	job, err := admin.BatchV1().Jobs(observerProjectA).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      map[string]string{"ai.compute/workload": name},
			Annotations: map[string]string{"ai.compute/generation": "1"},
			Finalizers:  []string{finalizer},
		},
		Spec: batchv1.JobSpec{
			Suspend: &suspended,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "workload", Image: "example.invalid/blocked@" + digest}},
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if job.UID == "" || job.ResourceVersion == "" {
		t.Fatalf("API server did not issue genuine Job UID/RV: %#v", job)
	}
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		// Remove the unique fixture finalizer even when t.Fatal aborts
		// the test. Both DELETEs are executed by test admin, not Observer.
		_, patchErr := admin.BatchV1().Jobs(observerProjectA).Patch(cleanupCtx, name,
			types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`), metav1.PatchOptions{})
		if patchErr != nil && !apierrors.IsNotFound(patchErr) {
			t.Logf("remove deletion-race finalizer: %v", patchErr)
		}
		if deleteErr := admin.BatchV1().Jobs(observerProjectA).Delete(cleanupCtx, name,
			metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &job.UID}}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			t.Logf("delete deletion-race test Job: %v", deleteErr)
		}
	}()

	yes := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "-synthetic-pod", Namespace: observerProjectA,
			UID: types.UID("synthetic-pod-for-second-get"), ResourceVersion: "fixture-pod-rv",
			Labels: map[string]string{"batch.kubernetes.io/controller-uid": string(job.UID)},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &yes,
			}},
		},
		Spec: corev1.PodSpec{
			NodeName:   nodeName,
			Containers: []corev1.Container{{Name: "workload", Image: "example.invalid/blocked@" + digest}},
		},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "workload", Ready: true,
				ImageID: "docker-pullable://example.invalid/blocked@" + digest,
			}},
		},
	}
	harness := &secondGetDeleteClient{
		Interface: reader, admin: admin, namespace: observerProjectA,
		jobName: name, jobUID: job.UID, pod: pod,
	}
	expected := ExpectedJob{
		Namespace: observerProjectA, Name: name, JobUID: job.UID,
		Generation: 1, ContainerName: "workload", ImageDigest: digest,
	}
	challenge := ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "real-kind",
		ProjectID: observerProjectA, WorkloadID: name,
		Generation: 1, JobUID: string(job.UID), Nonce: strings.Repeat("d", 64),
	}
	dispatchPub, dispatchKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rawTicket, err := AuthorObserverWorkTicket(ObserverWorkTicket{
		Challenge: challenge, Expected: expected,
		IssuedAt:  now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	}, ObserverSigner{KeyID: "real-kind-dispatch-fixture", Signer: dispatchKey})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ApproveObserverWorkTicket(rawTicket, ObserverTrust{
		KeyID: "real-kind-dispatch-fixture", PublicKey: dispatchPub,
	})
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof, observed, err := RunApprovedObserverWork(ctx, harness, approved, ObserverSigner{
		KeyID: "real-kind-observer-fixture", Signer: observerKey,
	})
	if err != nil || harness.jobReads != 2 || observed.Code != OwnershipUnproven {
		t.Fatalf("actual second GET deletion was promoted: %#v err=%v reads=%d",
			observed, err, harness.jobReads)
	}
	realDeleting, err := reader.BatchV1().Jobs(observerProjectA).Get(ctx, name, metav1.GetOptions{})
	if err != nil || realDeleting.UID != job.UID || realDeleting.DeletionTimestamp == nil ||
		realDeleting.ResourceVersion == job.ResourceVersion {
		t.Fatalf("real API server did not retain changed deleting Job: %#v err=%v", realDeleting, err)
	}
	verified, err := VerifyObservedJobEnvelope(proof, ObserverTrust{
		KeyID: "real-kind-observer-fixture", PublicKey: observerPub,
	}, challenge, expected)
	if err != nil || verified.Observation.Code != OwnershipUnproven ||
		verified.Observation.Code == PODReadyObserved {
		t.Fatalf("second-GET deletion evidence is falsely positive: %#v err=%v", verified, err)
	}
	t.Log("actual Job DELETE between signed Observer GETs was rejected; Pod Ready input was synthetic")
}
