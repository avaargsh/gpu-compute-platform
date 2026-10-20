package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	podE2EProject = "verifiedops-token-a"
	podE2EObserverNS = "verifiedops-observer"
)

// TestRealKindObserverPodSignsPendingEvidence executes the built, separate
// one-shot Observer binary in a real kind Pod with its projected short-lived
// ServiceAccount token. The real API server supplies Job UID/RV and Job GET;
// no fake client or admin credential is mounted in the Observer Pod.
// Ephemeral test keys are delivered through test-only Secrets and staged into
// owner-only files by an initContainer. This does not test production KMS/CSI,
// a real workload Pod becoming Ready, or PostgreSQL receipt admission.
func TestRealKindObserverPodSignsPendingEvidence(t *testing.T) {
	if os.Getenv("VERIFIEDOPS_OBSERVER_POD_E2E") != "1" {
		t.Skip("requires kind, the test Observer image and reviewed RBAC fixture")
	}
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

	dispatchPub, dispatchKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const jobName = "observer-pod-real-api-pending"
	const podName = "observer-pod-real-api-evidence"
	const secretName = "observer-pod-e2e-credentials"
	const keyID = "kind-ephemeral-observer-only"
	const dispatcherID = "kind-ephemeral-dispatcher-only"

	job, err := admin.BatchV1().Jobs(podE2EProject).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: jobName,
			Labels: map[string]string{"ai.compute/workload": jobName},
			Annotations: map[string]string{"ai.compute/generation": "1"},
		},
		Spec: batchv1.JobSpec{
			Suspend: func() *bool { v := true; return &v }(),
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{Name: "workload", Image: "example.invalid/blocked@" + digest}},
			}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		_ = admin.CoreV1().Pods(podE2EObserverNS).Delete(cleanup, podName, metav1.DeleteOptions{})
		_ = admin.CoreV1().Secrets(podE2EObserverNS).Delete(cleanup, secretName, metav1.DeleteOptions{})
		_ = admin.BatchV1().Jobs(podE2EProject).Delete(cleanup, jobName,
			metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &job.UID}})
	}()
	if job.UID == "" || job.ResourceVersion == "" {
		t.Fatalf("API server did not issue Job identity: %#v", job)
	}
	expected := verifiedops.ExpectedJob{
		Namespace: podE2EProject, Name: jobName, JobUID: job.UID,
		Generation: 1, ContainerName: "workload", ImageDigest: digest,
	}
	subject := verifiedops.ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "real-kind",
		ProjectID: podE2EProject, WorkloadID: jobName, Generation: 1,
		JobUID: string(job.UID), Nonce: strings.Repeat("b", 64),
	}
	now := time.Now().UTC()
	ticket, err := verifiedops.AuthorObserverWorkTicket(verifiedops.ObserverWorkTicket{
		Challenge: subject, Expected: expected,
		IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	}, verifiedops.ObserverSigner{KeyID: dispatcherID, Signer: dispatchKey})
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(observerKey)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	root := []byte(base64.StdEncoding.EncodeToString(dispatchPub))

	_, err = admin.CoreV1().Secrets(podE2EObserverNS).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: podE2EObserverNS},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ticket.dsse.json": ticket,
			"dispatcher.pub.b64": root,
			"observer.pk8.pem": privatePEM,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	falseValue := false
	_, err = admin.CoreV1().Pods(podE2EObserverNS).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName, Namespace: podE2EObserverNS,
			Labels: map[string]string{"app.kubernetes.io/component": "verifiedops-observer-e2e"},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "verifiedops-observer",
			AutomountServiceAccountToken: &falseValue,
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: func() *bool { v := true; return &v }(),
				RunAsUser: func() *int64 { v := int64(65532); return &v }(),
				RunAsGroup: func() *int64 { v := int64(65532); return &v }(),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			InitContainers: []corev1.Container{{
				Name: "stage-ephemeral-e2e-credentials",
				Image: "busybox:1.36",
				Command: []string{"sh", "-ec", "cp /secret/ticket.dsse.json /work/ticket.dsse.json; cp /secret/dispatcher.pub.b64 /work/dispatcher.pub.b64; cp /secret/observer.pk8.pem /work/observer.pk8.pem; chown 65532:65532 /work/*; chmod 0400 /work/observer.pk8.pem; chmod 0444 /work/ticket.dsse.json /work/dispatcher.pub.b64"},
				SecurityContext: &corev1.SecurityContext{
					RunAsUser: func() *int64 { v := int64(0); return &v }(),
					AllowPrivilegeEscalation: &falseValue,
					Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "credentials", MountPath: "/secret", ReadOnly: true},
					{Name: "staged", MountPath: "/work"},
				},
			}},
			Containers: []corev1.Container{{
				Name: "observer", Image: "verifiedops-observer:e2e", ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "VERIFIEDOPS_TICKET_FILE", Value: "/run/verifiedops/ticket/ticket.dsse.json"},
					{Name: "VERIFIEDOPS_DISPATCHER_PUBKEY_FILE", Value: "/run/verifiedops/root/dispatcher.pub.b64"},
					{Name: "VERIFIEDOPS_DISPATCHER_KEY_ID", Value: dispatcherID},
					{Name: "VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE", Value: "/run/verifiedops/identity/observer.pk8.pem"},
					{Name: "VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID", Value: keyID},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &falseValue, ReadOnlyRootFilesystem: &[]bool{true}[0],
					Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{"cpu": resource.MustParse("20m"), "memory": resource.MustParse("64Mi")},
					Limits: corev1.ResourceList{"cpu": resource.MustParse("250m"), "memory": resource.MustParse("256Mi")},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "kubernetes-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
					{Name: "staged", MountPath: "/run/verifiedops", ReadOnly: true},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "credentials", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName, DefaultMode: func() *int32 { v := int32(0444); return &v }()}}},
				{Name: "staged", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "kubernetes-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
					DefaultMode: func() *int32 { v := int32(0444); return &v }(),
					Sources: []corev1.VolumeProjection{
						{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: func() *int64 { v := int64(600); return &v }()}},
						{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
						{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}}}},
					},
				}}},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(100 * time.Second)
	for {
		current, getErr := admin.CoreV1().Pods(podE2EObserverNS).Get(ctx, podName, metav1.GetOptions{})
		if getErr != nil {
			t.Fatal(getErr)
		}
		if current.Status.Phase == corev1.PodSucceeded {
			break
		}
		if current.Status.Phase == corev1.PodFailed {
			logs, _ := admin.CoreV1().Pods(podE2EObserverNS).GetLogs(podName, &corev1.PodLogOptions{Container: "observer"}).DoRaw(ctx)
			t.Fatalf("independent Observer Pod failed: phase=%s reason=%s logs=%s", current.Status.Phase, current.Status.Reason, string(logs))
		}
		if time.Now().After(deadline) {
			t.Fatalf("Observer Pod did not finish; phase=%s reason=%s", current.Status.Phase, current.Status.Reason)
		}
		time.Sleep(500 * time.Millisecond)
	}
	logs, err := admin.CoreV1().Pods(podE2EObserverNS).GetLogs(podName, &corev1.PodLogOptions{Container: "observer"}).DoRaw(ctx)
	if err != nil {
		t.Fatal(err)
	}
	envelope := []byte(strings.TrimSpace(string(logs)))
	verified, err := verifiedops.VerifyObservedJobEnvelope(envelope,
		verifiedops.ObserverTrust{KeyID: keyID, PublicKey: observerPub}, subject, expected)
	if err != nil || verified.Observation.Code != verifiedops.OutcomePending ||
		verified.Observation.JobUID != job.UID {
		t.Fatalf("actual independent Pod did not emit verifiable pending evidence: %#v %v logs=%s", verified, err, string(logs))
	}
}
