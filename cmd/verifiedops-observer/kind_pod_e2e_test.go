package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// The real kind API server starts the real one-shot CLI in a separate Pod
// with a projected ServiceAccount token; CI independently verifies its DSSE.
// The signer and ticket are generated once in the test and stored briefly in
// an ephemeral *test-only* Secret. The initContainer copies them to an
// owner-only regular file because the CLI deliberately forbids symlinked
// Kubernetes Secret mounts. This is NOT a production signing key delivery,
// KMS/CSI acceptance, actual Ready GPU Pod, or PostgreSQL receipt test.
func TestRealKindObserverPodSignsPendingEvidence(t *testing.T) {
	if os.Getenv("VERIFIEDOPS_OBSERVER_POD_E2E") != "1" {
		t.Skip("requires a real kind cluster with the reviewed project Role and preloaded Observer test image")
	}
	const project = "verifiedops-token-a"
	const identityNamespace = "verifiedops-observer"
	const image = "verifiedops-observer:e2e"
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	suspended := true
	const jobName = "verifiedops-observer-pod-e2e-target"
	job, err := admin.BatchV1().Jobs(project).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Labels:      map[string]string{"ai.compute/workload": jobName},
			Annotations: map[string]string{"ai.compute/generation": "1"},
		},
		Spec: batchv1.JobSpec{
			Suspend: &suspended,
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy:                corev1.RestartPolicyNever,
				Containers:    []corev1.Container{{Name: "workload", Image: "example.invalid/test@" + digest}},
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil || job == nil || job.UID == "" || job.ResourceVersion == "" {
		t.Fatalf("real API-server CREATE did not return bound Job UID/RV: %v", err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := admin.BatchV1().Jobs(project).Delete(cleanup, job.Name,
			metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &job.UID}}); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup target Job: %v", err)
		}
	})

	dispatchPub, dispatchKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(observerKey)
	if err != nil {
		t.Fatal(err)
	}
	expected := verifiedops.ExpectedJob{
		Namespace: project, Name: job.Name, JobUID: job.UID,
		Generation: 1, ContainerName: "workload", ImageDigest: digest,
	}
	challenge := verifiedops.ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "real-kind-pod",
		ProjectID: project, WorkloadID: job.Name, Generation: 1,
		JobUID: string(job.UID), Nonce: strings.Repeat("e", 64),
	}
	now := time.Now().UTC()
	rawTicket, err := verifiedops.AuthorObserverWorkTicket(verifiedops.ObserverWorkTicket{
		Challenge: challenge, Expected: expected,
		IssuedAt:  now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(2*time.Minute - time.Second).Format(time.RFC3339Nano),
	}, verifiedops.ObserverSigner{KeyID: "kind-dispatcher-test-only", Signer: dispatchKey})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := admin.CoreV1().Secrets(identityNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "observer-pod-e2e-only-"},
		Type:       corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ticket.dsse.json":   rawTicket,
			"dispatcher.pub.b64": []byte(base64.StdEncoding.EncodeToString(dispatchPub)),
			"observer.pk8.pem":   pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := admin.CoreV1().Secrets(identityNamespace).Delete(cleanup, secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup test-only signing Secret: %v", err)
		}
	})

	nonroot, readOnly, denyEsc, automount := true, true, false, false
	uid, mode, tokenTTL := int64(65532), int32(0o444), int64(600)
	dropAll := []corev1.Capability{"ALL"}
	security := &corev1.SecurityContext{
		AllowPrivilegeEscalation: &denyEsc,
		ReadOnlyRootFilesystem:   &readOnly,
		Capabilities:             &corev1.Capabilities{Drop: dropAll},
	}
	pod, err := admin.CoreV1().Pods(identityNamespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "verifiedops-observer-signed-e2e-"},
		Spec: corev1.PodSpec{
			ServiceAccountName:           "verifiedops-observer",
			AutomountServiceAccountToken: &automount,
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &nonroot, RunAsUser: &uid, RunAsGroup: &uid,
				FSGroup: &uid, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			InitContainers: []corev1.Container{{
				Name: "stage-test-only-files", Image: image, ImagePullPolicy: corev1.PullNever,
				Command:         []string{"sh", "-ec", "cp /run/e2e-secret/ticket.dsse.json /run/verifiedops/ticket.dsse.json; cp /run/e2e-secret/dispatcher.pub.b64 /run/verifiedops/dispatcher.pub.b64; cp /run/e2e-secret/observer.pk8.pem /run/verifiedops/observer.pk8.pem; chmod 0400 /run/verifiedops/*"},
				SecurityContext: security,
				VolumeMounts: []corev1.VolumeMount{
					{Name: "test-fixture-secret", MountPath: "/run/e2e-secret", ReadOnly: true},
					{Name: "staged-files", MountPath: "/run/verifiedops"},
				},
			}},
			Containers: []corev1.Container{{
				Name: "observer", Image: image, ImagePullPolicy: corev1.PullNever,
				SecurityContext: security,
				Env: []corev1.EnvVar{
					{Name: "VERIFIEDOPS_TICKET_FILE", Value: "/run/verifiedops/ticket.dsse.json"},
					{Name: "VERIFIEDOPS_DISPATCHER_PUBKEY_FILE", Value: "/run/verifiedops/dispatcher.pub.b64"},
					{Name: "VERIFIEDOPS_DISPATCHER_KEY_ID", Value: "kind-dispatcher-test-only"},
					{Name: "VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE", Value: "/run/verifiedops/observer.pk8.pem"},
					{Name: "VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID", Value: "kind-observer-test-only"},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "staged-files", MountPath: "/run/verifiedops", ReadOnly: true},
					{Name: "api-identity", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "test-fixture-secret", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name, DefaultMode: &mode,
				}}},
				{Name: "staged-files", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "api-identity", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
					DefaultMode: &mode,
					Sources: []corev1.VolumeProjection{
						{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: &tokenTTL}},
						{ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
							Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
						}},
						{DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{{
								Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
							}},
						}},
					},
				}}},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if err := admin.CoreV1().Pods(identityNamespace).Delete(cleanup, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			t.Logf("cleanup Observer e2e Pod: %v", err)
		}
	})
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 110*time.Second, true,
		func(ctx context.Context) (bool, error) {
			live, getErr := admin.CoreV1().Pods(identityNamespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if getErr != nil {
				return false, getErr
			}
			switch live.Status.Phase {
			case corev1.PodSucceeded:
				return true, nil
			case corev1.PodFailed:
				return false, fmt.Errorf("real Observer Pod failed: %s", live.Status.Reason)
			default:
				return false, nil
			}
		})
	if err != nil {
		t.Fatalf("real Observer Pod did not complete with signed evidence: %v", err)
	}
	rawProof, err := admin.CoreV1().Pods(identityNamespace).GetLogs(pod.Name,
		&corev1.PodLogOptions{Container: "observer"}).DoRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(rawProof, []byte("\n")) != 1 {
		t.Fatalf("Observer emitted %d lines, expected exactly one DSSE evidence line", bytes.Count(rawProof, []byte("\n")))
	}
	verified, err := verifiedops.VerifyObservedJobEnvelope(bytes.TrimSpace(rawProof),
		verifiedops.ObserverTrust{KeyID: "kind-observer-test-only", PublicKey: observerPub},
		challenge, expected)
	if err != nil || verified.Observation.Code != verifiedops.OutcomePending ||
		verified.Subject.JobUID != string(job.UID) {
		t.Fatalf("independent Pod did not emit signed bound pending evidence: %#v err=%v", verified, err)
	}
	t.Log("real independent one-shot Observer Pod: projected SA + DSSE pending proof verified; ephemeral fixture only")
}
