package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avaargsh/gpu-compute-platform/internal/verifiedops"
	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

const testImageDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func fixtureCLI(t *testing.T) (observerConfig, *fake.Clientset, verifiedops.ObserverSubject, verifiedops.ExpectedJob, ed25519.PublicKey) {
	t.Helper()
	base := t.TempDir()
	public, dispatcher, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	observerPub, observerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	expected := verifiedops.ExpectedJob{
		Namespace: "reviewed-project", Name: "job-project-a", JobUID: types.UID("original-create-uid"), Generation: 1,
		ContainerName: "workload", ImageDigest: testImageDigest,
	}
	subject := verifiedops.ObserverSubject{
		OperationID: strings.Repeat("a", 32), ClusterID: "cluster-a", ProjectID: "project-a", WorkloadID: "project-a",
		Generation: 1, JobUID: string(expected.JobUID), Nonce: strings.Repeat("b", 64),
	}
	ticket := verifiedops.ObserverWorkTicket{
		Challenge: subject, Expected: expected,
		IssuedAt:  now.Add(-time.Second).Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	envelope, err := verifiedops.AuthorObserverWorkTicket(ticket, verifiedops.ObserverSigner{KeyID: "dispatcher", Signer: dispatcher})
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(observerPriv)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	cfg := observerConfig{
		ticketFile:              filepath.Join(base, "ticket.dsse.json"),
		dispatcherPublicKeyFile: filepath.Join(base, "root.pub.b64"),
		dispatcherKeyID:         "dispatcher",
		observerPrivateKeyFile:  filepath.Join(base, "observer.pk8.pem"),
		observerKeyID:           "observer",
	}
	for _, f := range []struct {
		name string
		data []byte
		perm os.FileMode
	}{
		{cfg.ticketFile, envelope, 0o600},
		{cfg.dispatcherPublicKeyFile, []byte(base64.StdEncoding.EncodeToString(public)), 0o600},
		{cfg.observerPrivateKeyFile, keyPEM, 0o600},
	} {
		if err := os.WriteFile(f.name, f.data, f.perm); err != nil {
			t.Fatal(err)
		}
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: expected.Name, Namespace: expected.Namespace, UID: expected.JobUID,
			ResourceVersion: "19", Labels: map[string]string{"ai.compute/workload": expected.Name},
			Annotations: map[string]string{"ai.compute/generation": "1"}},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "workload", Image: "registry.example/train@" + testImageDigest}},
		}}},
	}
	client := fake.NewSimpleClientset(job)
	client.PrependReactor("create", "selfsubjectreviews", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, &authenticationv1.SelfSubjectReview{Status: authenticationv1.SelfSubjectReviewStatus{
			UserInfo: authenticationv1.UserInfo{Username: verifiedops.ObserverIdentityUsername},
		}}, nil
	})
	return cfg, client, subject, expected, observerPub
}

func TestObserverOnceSignsOnlyRealPendingEvidence(t *testing.T) {
	cfg, client, subject, expected, observerPub := fixtureCLI(t)
	var out bytes.Buffer
	if err := runOnce(context.Background(), cfg, client, &out); err != nil {
		t.Fatal(err)
	}
	evidence, err := verifiedops.VerifyObservedJobEnvelope(bytes.TrimSpace(out.Bytes()),
		verifiedops.ObserverTrust{KeyID: "observer", PublicKey: observerPub}, subject, expected)
	if err != nil || evidence.Observation.Code != verifiedops.OutcomePending {
		t.Fatalf("not a signed real pending read: %#v %v", evidence, err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" && a.GetResource().Resource != "selfsubjectreviews" {
			t.Fatalf("one-shot observer mutated Kubernetes: %v", a)
		}
	}
}

func TestObserverOnceFailClosedForWorldReadableKeyAndSymlink(t *testing.T) {
	for _, mode := range []string{"world-readable", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			cfg, client, _, _, _ := fixtureCLI(t)
			switch mode {
			case "world-readable":
				if err := os.Chmod(cfg.observerPrivateKeyFile, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := cfg.observerPrivateKeyFile
				link := filepath.Join(filepath.Dir(target), "linked-private-key")
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
				cfg.observerPrivateKeyFile = link
			}
			var out bytes.Buffer
			if err := runOnce(context.Background(), cfg, client, &out); err == nil || out.Len() != 0 {
				t.Fatalf("insecure signer produced evidence: %v", err)
			}
			if len(client.Actions()) != 0 {
				t.Fatalf("rejected signer still accessed Kubernetes: %v", client.Actions())
			}
		})
	}
}

func TestObserverConfigRequiresExternalAbsoluteIdentityPaths(t *testing.T) {
	env := map[string]string{
		"VERIFIEDOPS_TICKET_FILE":               "/ticket",
		"VERIFIEDOPS_DISPATCHER_PUBKEY_FILE":    "/root-pub",
		"VERIFIEDOPS_DISPATCHER_KEY_ID":         "dispatcher",
		"VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE": "/signer",
		"VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID":   "observer",
	}
	get := func(k string) string { return env[k] }
	if _, err := configFromEnv(get); err != nil {
		t.Fatal(err)
	}
	env["VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE"] = "../relative-key"
	if _, err := configFromEnv(get); err == nil {
		t.Fatal("relative key path was accepted")
	}
	env["VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE"] = "/ticket"
	if _, err := configFromEnv(get); err == nil {
		t.Fatal("same file used for ticket and private key")
	}
}

func TestDisabledExampleObserverJobHasProjectedTokenAndNoPrivilegedHostPath(t *testing.T) {
	content, err := os.ReadFile("../../deploy/kubernetes/verifiedops-observer/observer-job.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := yaml.UnmarshalStrict(content, &job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Suspend == nil || !*job.Spec.Suspend || job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatal("example Job must be suspended and must not retry")
	}
	pod := job.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken || pod.ServiceAccountName != "verifiedops-observer" ||
		pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("untrusted Pod inherits token or wrong identity")
	}
	if len(pod.Containers) != 1 || pod.Containers[0].SecurityContext == nil {
		t.Fatal("must have one isolated Observer container")
	}
	c := pod.Containers[0]
	checks := []struct {
		name string
		bad  bool
	}{
		{"allowPrivilegeEscalation", c.SecurityContext.AllowPrivilegeEscalation == nil || *c.SecurityContext.AllowPrivilegeEscalation},
		{"readOnlyRootFilesystem", c.SecurityContext.ReadOnlyRootFilesystem == nil || !*c.SecurityContext.ReadOnlyRootFilesystem},
		{"privileged", c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged},
		{"runAsNonRoot", c.SecurityContext.RunAsNonRoot == nil || !*c.SecurityContext.RunAsNonRoot},
		{"runAsUser", c.SecurityContext.RunAsUser == nil || *c.SecurityContext.RunAsUser != 65532},
		{"runAsGroup", c.SecurityContext.RunAsGroup == nil || *c.SecurityContext.RunAsGroup != 65532},
		{"dropAllCapabilities", c.SecurityContext.Capabilities == nil || len(c.SecurityContext.Capabilities.Drop) != 1 || c.SecurityContext.Capabilities.Drop[0] != "ALL"},
		{"ports", len(c.Ports) != 0},
		{"envFrom", len(c.EnvFrom) != 0},
		{"command", len(c.Command) != 0},
		{"args", len(c.Args) != 0},
		{"imageDigest", !strings.Contains(c.Image, "@sha256:")},
	}
	for _, check := range checks {
		if check.bad {
			t.Errorf("sample workload violates %s: container=%#v", check.name, c)
		}
	}
	if t.Failed() {
		t.Fatal("sample workload has unsafe container security settings or overridable process")
	}
	// The Observer must receive only the ticket, pinned dispatcher public root,
	// projected short-lived API identity and the externally provisioned signer.
	// In particular, it must never inherit database credentials or arbitrary envFrom.
	wantEnv := map[string]bool{
		"VERIFIEDOPS_TICKET_FILE":               true,
		"VERIFIEDOPS_DISPATCHER_PUBKEY_FILE":    true,
		"VERIFIEDOPS_DISPATCHER_KEY_ID":         true,
		"VERIFIEDOPS_OBSERVER_SIGNING_KEY_FILE": true,
		"VERIFIEDOPS_OBSERVER_SIGNING_KEY_ID":   true,
	}
	if len(c.Env) != len(wantEnv) {
		t.Fatalf("unexpected Observer environment surface: %#v", c.Env)
	}
	for _, env := range c.Env {
		if !wantEnv[env.Name] || env.ValueFrom != nil {
			t.Fatalf("unapproved Observer environment source: %#v", env)
		}
		delete(wantEnv, env.Name)
	}
	if len(wantEnv) != 0 {
		t.Fatalf("required Observer environment variables missing: %#v", wantEnv)
	}
	if pod.SecurityContext.RunAsUser == nil || *pod.SecurityContext.RunAsUser != 65532 ||
		pod.SecurityContext.RunAsGroup == nil || *pod.SecurityContext.RunAsGroup != 65532 ||
		pod.SecurityContext.SeccompProfile == nil || pod.SecurityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Fatal("sample Pod must use the reviewed non-root UID/GID and RuntimeDefault seccomp")
	}
	if len(pod.Volumes) != 4 {
		t.Fatalf("unexpected Observer volume surface: %#v", pod.Volumes)
	}
	hasProjected, hasCSI, hasTicket, hasRoot := false, false, false, false
	for _, volume := range pod.Volumes {
		if volume.HostPath != nil {
			t.Fatal("hostPath not allowed")
		}
		switch volume.Name {
		case "kubernetes-api-access":
			if volume.Projected == nil || volume.Secret != nil || volume.ConfigMap != nil || volume.CSI != nil {
				t.Fatal("API identity must be an explicit projected volume")
			}
			for _, src := range volume.Projected.Sources {
				if src.ServiceAccountToken != nil && src.ServiceAccountToken.ExpirationSeconds != nil &&
					*src.ServiceAccountToken.ExpirationSeconds <= 600 {
					hasProjected = true
				}
			}
		case "signed-work-ticket":
			if volume.Secret == nil || volume.Secret.SecretName != "verifiedops-observer-once-ticket" ||
				volume.Secret.Optional == nil || *volume.Secret.Optional {
				t.Fatal("ticket must come from the required, dedicated ticket Secret")
			}
			hasTicket = true
		case "pinned-dispatcher-root":
			if volume.ConfigMap == nil || volume.ConfigMap.Name != "verifiedops-observer-dispatcher-pinned-root" ||
				volume.ConfigMap.Optional == nil || *volume.ConfigMap.Optional {
				t.Fatal("dispatcher trust root must be pinned in the required ConfigMap")
			}
			hasRoot = true
		case "external-signer":
			if volume.CSI == nil || volume.CSI.Driver != "secrets-store.csi.k8s.io" ||
				volume.CSI.ReadOnly == nil || !*volume.CSI.ReadOnly ||
				volume.CSI.VolumeAttributes["secretProviderClass"] != "verifiedops-observer-external-signing-key" {
				t.Fatal("signer must come from the reviewed read-only external CSI provider")
			}
			hasCSI = true
		default:
			t.Fatalf("unapproved Observer volume: %q", volume.Name)
		}
	}
	if !hasProjected || !hasCSI || !hasTicket || !hasRoot {
		t.Fatal("need bounded projected token, required ticket/root and read-only external signer mount")
	}
	wantMounts := map[string]string{
		"kubernetes-api-access":  "/var/run/secrets/kubernetes.io/serviceaccount",
		"signed-work-ticket":     "/run/verifiedops/ticket",
		"pinned-dispatcher-root": "/run/verifiedops/root",
		"external-signer":        "/run/verifiedops/identity",
	}
	if len(c.VolumeMounts) != len(wantMounts) {
		t.Fatalf("unexpected Observer mount surface: %#v", c.VolumeMounts)
	}
	for _, mount := range c.VolumeMounts {
		wantPath, ok := wantMounts[mount.Name]
		if !ok || mount.MountPath != wantPath || !mount.ReadOnly {
			t.Fatalf("unapproved or writable Observer mount: %#v", mount)
		}
		delete(wantMounts, mount.Name)
	}
	if len(wantMounts) != 0 {
		t.Fatalf("required Observer mounts missing: %#v", wantMounts)
	}
	// Ensure no bundled key material or unreviewed container process starts.
	if bytes.Contains(content, []byte("BEGIN PRIVATE KEY")) {
		t.Fatal("key material in manifest")
	}
	if _, err := json.Marshal(job); err != nil {
		t.Fatal(err)
	}
}

func TestObserverOnceRejectsHardLinkedSignerInput(t *testing.T) {
	for _, source := range []string{"ticket", "root"} {
		t.Run(source, func(t *testing.T) {
			cfg, client, _, _, _ := fixtureCLI(t)
			var input string
			switch source {
			case "ticket":
				input = cfg.ticketFile
			case "root":
				input = cfg.dispatcherPublicKeyFile
			}
			// A hardlink bypasses O_NOFOLLOW and Clean(path) equality.
			// Alias the already-opened input to the key's inode.
			if err := os.Remove(input); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(cfg.observerPrivateKeyFile, input); err != nil {
				t.Fatal(err)
			}
			if err := rejectSignerInputAlias(cfg); err == nil {
				t.Fatal("hardlinked key alias passed inode identity validation")
			}
			var output bytes.Buffer
			if err := runOnce(context.Background(), cfg, client, &output); err == nil {
				t.Fatal("hardlinked signing key accepted as untrusted input")
			}
			if output.Len() != 0 || len(client.Actions()) != 0 {
				t.Fatalf("hardlinked key reached Kubernetes or emitted proof: actions=%v", client.Actions())
			}
		})
	}
}
