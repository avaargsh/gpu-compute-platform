package verifiedops

import (
	"context"
	"fmt"
	"os"
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
}
