package kueue

import (
	"context"
	"errors"
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/domain"
	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func logSourceFixture() (baseprovider.WorkloadProjection, *batchv1.Job, *corev1.Pod) {
	scope := baseprovider.WorkloadProjection{
		ClusterID: "cluster-a", ProjectID: "project-a", WorkloadID: "train-1",
		Namespace: "ns-project-a", Generation: 7,
	}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "job-train-1", Namespace: scope.Namespace,
			UID: types.UID("job-uid-7"), ResourceVersion: "107",
			Labels: map[string]string{"ai.compute/workload": "job-train-1"},
			Annotations: map[string]string{
				generationAnnotation:    "7",
				logWorkloadIDAnnotation: "train-1",
				logProjectIDAnnotation:  "project-a",
			},
		},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload"}}},
		}},
	}
	isController := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "job-train-1-pod", Namespace: scope.Namespace,
			UID: types.UID("pod-uid-7"), ResourceVersion: "208",
			Labels: map[string]string{"ai.compute/workload": "job-train-1"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "batch/v1", Kind: "Job", Name: job.Name,
				UID: job.UID, Controller: &isController,
			}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "workload"}}},
	}
	return scope, job, pod
}

func TestInspectWorkloadLogSourceUsesExactOwnedKubernetesUIDsOnly(t *testing.T) {
	scope, job, pod := logSourceFixture()
	client := kubefake.NewSimpleClientset(job, pod)
	src, err := NewKubeClient(client, nil).InspectWorkloadLogSource(context.Background(), scope)
	if err != nil {
		t.Fatalf("expected an inspectable source: %v", err)
	}
	if src.ClusterID != scope.ClusterID || src.ProjectID != scope.ProjectID ||
		src.WorkloadID != scope.WorkloadID || src.Generation != 7 ||
		src.Namespace != scope.Namespace || src.JobName != job.Name ||
		src.JobUID != job.UID || src.JobResourceVersion != "107" ||
		src.PodName != pod.Name || src.PodUID != pod.UID ||
		src.PodResourceVersion != "208" || src.ContainerName != "workload" {
		t.Fatalf("wrong trusted source: %#v", src)
	}
	actions := client.Actions()
	if len(actions) != 2 || !actions[0].Matches("get", "jobs") || !actions[1].Matches("list", "pods") {
		t.Fatalf("source inspection must perform exactly one Job GET and Pod LIST: %#v", actions)
	}
}

func TestInspectWorkloadLogSourceRejectsIdentityDrift(t *testing.T) {
	cases := []struct {
		name   string
		change func(*baseprovider.WorkloadProjection, *batchv1.Job, *corev1.Pod)
	}{
		{"job-newer-generation", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			j.Annotations[generationAnnotation] = "8"
		}},
		{"job-no-generation", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			delete(j.Annotations, generationAnnotation)
		}},
		{"job-missing-uid", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) { j.UID = "" }},
		{"job-missing-rv", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) { j.ResourceVersion = "" }},
		{"job-wrong-label", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			j.Labels["ai.compute/workload"] = "job-other"
		}},
		{"job-missing-workload-id", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			delete(j.Annotations, logWorkloadIDAnnotation)
		}},
		{"job-colliding-workload-id", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			j.Annotations[logWorkloadIDAnnotation] = "train_1"
		}},
		{"job-cross-project", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			j.Annotations[logProjectIDAnnotation] = "project-b"
		}},
		{"job-deleting", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			now := metav1.Now()
			j.DeletionTimestamp = &now
		}},
		{"job-no-container", func(_ *baseprovider.WorkloadProjection, j *batchv1.Job, _ *corev1.Pod) {
			j.Spec.Template.Spec.Containers = nil
		}},
		{"pod-old-job-uid", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) {
			p.OwnerReferences[0].UID = "old-job-uid"
		}},
		{"pod-not-controller", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) {
			no := false
			p.OwnerReferences[0].Controller = &no
		}},
		{"pod-wrong-kind", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) {
			p.OwnerReferences[0].Kind = "ReplicaSet"
		}},
		{"pod-no-owner", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) { p.OwnerReferences = nil }},
		{"pod-missing-uid", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) { p.UID = "" }},
		{"pod-missing-rv", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) { p.ResourceVersion = "" }},
		{"pod-no-container", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) {
			p.Spec.Containers = []corev1.Container{{Name: "other"}}
		}},
		{"pod-deleting", func(_ *baseprovider.WorkloadProjection, _ *batchv1.Job, p *corev1.Pod) {
			now := metav1.Now()
			p.DeletionTimestamp = &now
		}},
		{"wrong-project-scope", func(s *baseprovider.WorkloadProjection, _ *batchv1.Job, _ *corev1.Pod) { s.ProjectID = "project-b" }},
		{"wrong-namespace", func(s *baseprovider.WorkloadProjection, _ *batchv1.Job, _ *corev1.Pod) { s.Namespace = "ns-project-b" }},
		{"invalid-generation", func(s *baseprovider.WorkloadProjection, _ *batchv1.Job, _ *corev1.Pod) { s.Generation = 0 }},
		{"aliased-workload-id", func(s *baseprovider.WorkloadProjection, _ *batchv1.Job, _ *corev1.Pod) { s.WorkloadID = "train_1" }},
		{"invalid-namespace", func(s *baseprovider.WorkloadProjection, _ *batchv1.Job, _ *corev1.Pod) {
			s.Namespace = "../ns-project-a"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, job, pod := logSourceFixture()
			tc.change(&scope, job, pod)
			client := kubefake.NewSimpleClientset(job, pod)
			_, err := NewKubeClient(client, nil).InspectWorkloadLogSource(context.Background(), scope)
			if err == nil {
				t.Fatal("unowned, stale or colliding log source must fail closed")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" {
					t.Fatalf("inspection must never mutate or read logs: %s %s", action.GetVerb(), action.GetResource().Resource)
				}
			}
		})
	}
}

func TestInspectWorkloadLogSourceRejectsZeroAndMultiplePods(t *testing.T) {
	for _, count := range []int{0, 2} {
		scope, job, pod := logSourceFixture()
		objects := []runtime.Object{job}
		for i := 0; i < count; i++ {
			next := pod.DeepCopy()
			if i > 0 {
				next.Name = "duplicate-pod"
				next.UID = types.UID("duplicate-uid")
			}
			objects = append(objects, next)
		}
		client := kubefake.NewSimpleClientset(objects...)
		if _, err := NewKubeClient(client, nil).InspectWorkloadLogSource(context.Background(), scope); err == nil {
			t.Fatalf("%d candidate pods must be an unavailable/ambiguous source", count)
		}
	}
}

func TestInspectWorkloadLogSourceDoesNotFallbackOnForbiddenKubernetesRead(t *testing.T) {
	for _, target := range []string{"jobs", "pods"} {
		scope, job, pod := logSourceFixture()
		client := kubefake.NewSimpleClientset(job, pod)
		client.PrependReactor("get", "jobs", func(ktesting.Action) (bool, runtime.Object, error) {
			if target == "jobs" {
				return true, nil, apierrors.NewForbidden(batchv1.Resource("jobs"), job.Name, errors.New("denied"))
			}
			return false, nil, nil
		})
		client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
			if target == "pods" {
				return true, nil, apierrors.NewForbidden(corev1.Resource("pods"), pod.Name, errors.New("denied"))
			}
			return false, nil, nil
		})
		if _, err := NewKubeClient(client, nil).InspectWorkloadLogSource(context.Background(), scope); err == nil {
			t.Fatalf("Kubernetes %s forbidden must fail closed", target)
		}
	}
}

func TestNewKueueJobsRetainExactProjectAndWorkloadIdentity(t *testing.T) {
	for _, allocation := range []string{domain.AcceleratorAllocationExtendedResource, domain.AcceleratorAllocationDRA} {
		in := baseprovider.WorkloadProjection{
			WorkloadID: "train-1", ProjectID: "project-a", PoolID: "pool-1",
			ClusterID: "cluster-a", Namespace: "ns-project-a", Generation: 7,
			Image:       "example/train:latest",
			Accelerator: domain.AcceleratorRequest{Class: "gpu", Quota: 1},
			AcceleratorBinding: domain.AcceleratorBinding{
				Class: "gpu", Flavor: "gpu-flavor", AllocationMode: allocation,
				ResourceName: "nvidia.com/gpu",
				DRA:          &domain.DRAAllocation{DeviceClassName: "gpu.example"},
			},
		}
		job, err := ProjectWorkload(in)
		if err != nil {
			t.Fatal(err)
		}
		obj, err := buildJobObject(job)
		if err != nil {
			t.Fatal(err)
		}
		if obj.Annotations[logProjectIDAnnotation] != string(in.ProjectID) ||
			obj.Annotations[logWorkloadIDAnnotation] != string(in.WorkloadID) ||
			obj.Annotations[generationAnnotation] != "7" {
			t.Fatalf("project/Workload identity lost in Job: %#v", obj.Annotations)
		}
	}
}
