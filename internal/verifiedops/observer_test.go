package verifiedops

import (
	"context"
	"errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	fake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
)

const sha = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func fixture() (ExpectedJob, *batchv1.Job, *corev1.Pod, *corev1.Node) {
	w := ExpectedJob{Namespace: "research", Name: "job-a", JobUID: types.UID("job-uid-a"), Generation: 7, ContainerName: "workload", ImageDigest: sha}
	yes := true
	j := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: w.Name, Namespace: w.Namespace, UID: w.JobUID, ResourceVersion: "42", Labels: map[string]string{"ai.compute/workload": w.Name}, Annotations: map[string]string{"ai.compute/generation": "7"}}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload", Image: "repo/train@" + sha}}}}}}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "job-a-pod", Namespace: w.Namespace, UID: types.UID("pod-uid-a"), ResourceVersion: "43", Labels: map[string]string{"batch.kubernetes.io/controller-uid": string(j.UID)}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: j.Name, UID: j.UID, Controller: &yes}}}, Spec: corev1.PodSpec{NodeName: "gpu-node-1", Containers: []corev1.Container{{Name: "workload", Image: "repo/train@" + sha}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "workload", Ready: true, ImageID: "docker-pullable://repo/train@" + sha}}}}
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gpu-node-1", UID: types.UID("node-uid-1"), ResourceVersion: "17"}, Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	return w, j, p, n
}
func observe(w ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) (Observation, error, []ktesting.Action) {
	c := fake.NewSimpleClientset(j, p, n)
	got, err := NewJobObserver(c).Observe(context.Background(), w)
	return got, err, c.Actions()
}
func TestExactBoundSinglePodCanOnlyBeObservedNotVerified(t *testing.T) {
	w, j, p, n := fixture()
	r, err, actions := observe(w, j, p, n)
	if err != nil || r.Code != PODReadyObserved || r.JobUID != j.UID || r.PodUID != p.UID || r.NodeUID != n.UID {
		t.Fatalf("unexpected observation %#v err %v", r, err)
	}
	if r.Code == "AUTHORIZED_AND_VERIFIED" {
		t.Fatal("read-only observations are not signed operation proof")
	}
	for _, a := range actions {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("observer mutated API: %s", a.GetVerb())
		}
	}
}
func TestBoundUIDGenerationAndOwnerFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*ExpectedJob, *batchv1.Job, *corev1.Pod, *corev1.Node)
		want  string
	}{
		{"recreated-job-uid", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) { j.UID = "new-uid" }, OwnershipUnproven},
		{"generation-drift", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			j.Annotations["ai.compute/generation"] = "8"
		}, OwnershipUnproven},
		{"wrong-controller-ref", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			p.OwnerReferences[0].UID = "foreign"
		}, OutcomePending},
		{"spoofed-ready-label", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) { p.OwnerReferences = nil }, OutcomePending},
		{"not-ready", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			p.Status.Conditions[0].Status = corev1.ConditionFalse
		}, OutcomePending},
		{"unscheduled", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) { p.Spec.NodeName = "" }, OutcomePending},
		{"node-not-ready", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			n.Status.Conditions[0].Status = corev1.ConditionFalse
		}, OutcomePending},
		{"node-cordoned", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			n.Spec.Unschedulable = true
		}, OutcomePending},
		{"node-deleting", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			now := metav1.Now()
			n.DeletionTimestamp = &now
		}, OutcomePending},
		{"wrong-image-digest", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			p.Status.ContainerStatuses[0].ImageID = "docker-pullable://repo/train@sha256:" + strings.Repeat("b", 64)
		}, OutcomeMismatch},
		{"tag-not-pinned", func(w *ExpectedJob, j *batchv1.Job, p *corev1.Pod, n *corev1.Node) {
			j.Spec.Template.Spec.Containers[0].Image = "repo/train:latest"
		}, OutcomeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, j, p, n := fixture()
			tc.alter(&w, j, p, n)
			got, err, _ := observe(w, j, p, n)
			if err != nil || got.Code != tc.want {
				t.Fatalf("got %#v error %v, want %s", got, err, tc.want)
			}
		})
	}
}
func TestInvalidBindingRejectsBeforeAnyKubernetesCall(t *testing.T) {
	w, j, p, n := fixture()
	for _, mut := range []func(*ExpectedJob){
		func(x *ExpectedJob) { x.JobUID = "" }, func(x *ExpectedJob) { x.Generation = 0 },
		func(x *ExpectedJob) { x.ImageDigest = "sha256:untrusted" },
		func(x *ExpectedJob) { x.ContainerName = "" }, func(x *ExpectedJob) { x.Namespace = "" }} {
		candidate := w
		mut(&candidate)
		c := fake.NewSimpleClientset(j, p, n)
		_, err := NewJobObserver(c).Observe(context.Background(), candidate)
		if err == nil || len(c.Actions()) != 0 {
			t.Fatalf("invalid binding reached API: %v %#v", err, c.Actions())
		}
	}
}
func TestNotFoundIsUnavailableNotDeletionProof(t *testing.T) {
	w, _, _, _ := fixture()
	c := fake.NewSimpleClientset()
	got, err := NewJobObserver(c).Observe(context.Background(), w)
	if err != nil || got.Code != VerificationUnavailable {
		t.Fatalf("404 must fail closed: %#v %v", got, err)
	}
}
func TestObserverReadErrorDoesNotBecomeSuccess(t *testing.T) {
	w, j, p, n := fixture()
	c := fake.NewSimpleClientset(j, p, n)
	c.PrependReactor("get", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(batchv1.Resource("jobs"), w.Name, errors.New("read denied"))
	})
	got, err := NewJobObserver(c).Observe(context.Background(), w)
	if err == nil || got.Code != VerificationUnavailable {
		t.Fatalf("reader error accepted: %#v %v", got, err)
	}
}
