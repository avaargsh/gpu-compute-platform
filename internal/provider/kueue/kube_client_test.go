package kueue

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKubeClientPoolAndWorkloadGoldenPath(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		workloadGVR: "WorkloadList",
	})
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, dynamicClient)

	if err := client.ApplyResourceFlavor(ctx, ResourceFlavor{
		Name:         "accel-h100-80g",
		ResourceName: "vendor.example/gpu",
		NodeLabels:   map[string]string{"ai.compute/accelerator-class": "h100-80g"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyClusterQueue(ctx, ClusterQueue{
		Name: "cq-pool-h100",
		Quotas: []ResourceQuota{
			{Flavor: "accel-h100-80g", Resource: "vendor.example/gpu", Nominal: 8},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyLocalQueue(ctx, LocalQueue{
		Name:         "lq-pool-h100",
		Namespace:    "project-1",
		ClusterQueue: "cq-pool-h100",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyJob(ctx, Job{
		Name:      "job-train-1",
		Namespace: "project-1",
		QueueName: "lq-pool-h100",
		Image:     "example/train:latest",
		Resources: map[string]int64{"vendor.example/gpu": 2},
		Annotations: map[string]string{
			"kueue.x-k8s.io/queue-name": "lq-pool-h100",
		},
	}); err != nil {
		t.Fatal(err)
	}

	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-train-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Active = 1
	updatedJob, err := coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	workload := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1",
		"kind":       "Workload",
		"metadata": map[string]any{
			"name":      "job-train-1-workload",
			"namespace": "project-1",
		},
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "QuotaReserved", "status": "True"},
				map[string]any{"type": "Admitted", "status": "True"},
			},
		},
	}}
	workload.SetOwnerReferences([]metav1.OwnerReference{{Kind: "Job", UID: updatedJob.UID}})
	if _, err := dynamicClient.Resource(workloadGVR).Namespace("project-1").Create(ctx, workload, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	_, err = coreClient.CoreV1().Pods("project-1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "job-train-1-pod",
			Labels: map[string]string{"ai.compute/workload": "job-train-1"},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	observed, err := client.ObserveJob(ctx, "project-1", "job-train-1")
	if err != nil {
		t.Fatal(err)
	}
	if !observed.QuotaReserved || !observed.Admitted || !observed.PodsReady || observed.Phase != "Running" {
		t.Fatalf("unexpected observation: %#v", observed)
	}

	obj, err := dynamicClient.Resource(clusterQueueGVR).Get(ctx, "cq-pool-h100", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if obj.GetKind() != "ClusterQueue" {
		t.Fatalf("unexpected cluster queue object: %#v", obj.Object)
	}
}

func TestPodReady(t *testing.T) {
	if !podReady([]corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}) {
		t.Fatal("expected ready pod")
	}
	if podReady([]corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}) {
		t.Fatal("expected non-ready pod")
	}
}

func TestObserveJobDoesNotInferAdmissionFromRunningReadyPod(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{workloadGVR: "WorkloadList"},
	))

	if err := client.ApplyJob(ctx, Job{
		Name: "job-no-admission", Namespace: "project-1", QueueName: "lq-pool",
		Image: "example/train:latest", Resources: map[string]int64{"vendor.example/gpu": 1},
	}); err != nil {
		t.Fatal(err)
	}
	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-no-admission", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Active = 1
	if _, err := coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := coreClient.CoreV1().Pods("project-1").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ready-pod", Labels: map[string]string{"ai.compute/workload": "job-no-admission"}},
		Status:     corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	observed, err := client.ObserveJob(ctx, "project-1", "job-no-admission")
	if err != nil {
		t.Fatal(err)
	}
	if observed.QuotaReserved || observed.Admitted {
		t.Fatalf("running ready pod must not imply Kueue quota or admission: %#v", observed)
	}
	if !observed.PodsReady || observed.Phase != "Running" {
		t.Fatalf("expected running ready workload: %#v", observed)
	}
}

func TestJobObjectKeepsQueueLabelOffPodTemplate(t *testing.T) {
	job, err := jobObject(Job{
		Name:      "job-train-1",
		Namespace: "project-1",
		Image:     "example/train:latest",
		Resources: map[string]int64{"vendor.example/gpu": 1},
		Labels:    map[string]string{"kueue.x-k8s.io/queue-name": "lq-pool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Labels["kueue.x-k8s.io/queue-name"] != "lq-pool" {
		t.Fatalf("job queue label missing: %#v", job.Labels)
	}
	if _, ok := job.Spec.Template.Labels["kueue.x-k8s.io/queue-name"]; ok {
		t.Fatalf("queue label must not leak to pod template: %#v", job.Spec.Template.Labels)
	}
	if job.Spec.Template.Labels["ai.compute/workload"] != "job-train-1" {
		t.Fatalf("pod workload identity missing: %#v", job.Spec.Template.Labels)
	}
}

func TestApplyJobForbiddenGetDoesNotCreate(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	createCalled := false
	coreClient.PrependReactor("get", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(batchv1.Resource("jobs"), "job-denied", errors.New("denied"))
	})
	coreClient.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		createCalled = true
		return false, nil, nil
	})

	client := NewKubeClient(coreClient, nil)
	err := client.ApplyJob(ctx, Job{
		Name:      "job-denied",
		Namespace: "project-1",
		Image:     "example/train:latest",
		Resources: map[string]int64{"vendor.example/gpu": 1},
	})
	if err == nil || !apierrors.IsForbidden(err) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
	if createCalled {
		t.Fatal("forbidden GET must not fall back to CREATE")
	}
}

func TestObserveSucceededJobPreservesKueueEvidenceWithoutInferringPodsReady(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		workloadGVR: "WorkloadList",
	})
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, dynamicClient)

	if err := client.ApplyJob(ctx, Job{
		Name: "job-success", Namespace: "project-1", QueueName: "lq-pool",
		Image: "example/train:latest", Resources: map[string]int64{"vendor.example/gpu": 1},
	}); err != nil {
		t.Fatal(err)
	}
	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-success", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	job, err = coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	workload := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1", "kind": "Workload",
		"metadata": map[string]any{"name": "job-success-workload", "namespace": "project-1"},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "QuotaReserved", "status": "True"},
			map[string]any{"type": "Admitted", "status": "True"},
		}},
	}}
	workload.SetOwnerReferences([]metav1.OwnerReference{{Kind: "Job", UID: job.UID}})
	if _, err := dynamicClient.Resource(workloadGVR).Namespace("project-1").Create(ctx, workload, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	got, err := client.ObserveJob(ctx, "project-1", "job-success")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "Succeeded" || !got.Succeeded || got.Failed {
		t.Fatalf("unexpected terminal state: %#v", got)
	}
	if got.PodsReady {
		t.Fatalf("succeeded job must not imply current pod readiness: %#v", got)
	}
	if !got.QuotaReserved || !got.Admitted || got.WorkloadName != "job-success-workload" {
		t.Fatalf("terminal job lost kueue evidence: %#v", got)
	}
}

func TestObserveFailedJobPreservesKueueEvidence(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		workloadGVR: "WorkloadList",
	})
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, dynamicClient)

	if err := client.ApplyJob(ctx, Job{
		Name: "job-failed", Namespace: "project-1", QueueName: "lq-pool",
		Image: "example/train:latest", Resources: map[string]int64{"vendor.example/gpu": 1},
	}); err != nil {
		t.Fatal(err)
	}
	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-failed", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Failed = 1
	job, err = coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	workload := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kueue.x-k8s.io/v1beta1", "kind": "Workload",
		"metadata": map[string]any{"name": "job-failed-workload", "namespace": "project-1"},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "QuotaReserved", "status": "True"},
			map[string]any{"type": "Admitted", "status": "True"},
		}},
	}}
	workload.SetOwnerReferences([]metav1.OwnerReference{{Kind: "Job", UID: job.UID}})
	if _, err := dynamicClient.Resource(workloadGVR).Namespace("project-1").Create(ctx, workload, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	got, err := client.ObserveJob(ctx, "project-1", "job-failed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "Failed" || !got.Failed || got.Succeeded {
		t.Fatalf("unexpected terminal state: %#v", got)
	}
	if !got.QuotaReserved || !got.Admitted || got.WorkloadName != "job-failed-workload" {
		t.Fatalf("failed job lost kueue evidence: %#v", got)
	}
}

func TestApplyExistingJobPreservesControllerStatus(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, nil)
	in := Job{
		Name: "job-preserve-status", Namespace: "project-1", QueueName: "lq-pool",
		Image: "busybox:1.36", Resources: map[string]int64{"vendor.example/gpu": 1},
	}
	if err := client.ApplyJob(ctx, in); err != nil {
		t.Fatal(err)
	}
	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, in.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	if _, err := coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyJob(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, in.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.Succeeded != 1 {
		t.Fatalf("reconcile erased controller-owned job status: %#v", got.Status)
	}
}


func TestApplyJobReplayIsIdempotentForSameGeneration(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, nil)
	in := Job{
		Name: "job-replay", Namespace: "project-1", Image: "busybox:1.36",
		Resources: map[string]int64{"vendor.example/gpu": 1},
		Annotations: map[string]string{"ai.compute/generation": "7"},
	}
	if err := client.ApplyJob(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyJob(ctx, in); err != nil {
		t.Fatalf("same-generation replay must be a no-op: %v", err)
	}
	jobs, err := coreClient.BatchV1().Jobs("project-1").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("replay created duplicate execution objects: %d", len(jobs.Items))
	}
}

func TestApplyJobFailsClosedOnGenerationMismatch(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, nil)
	base := Job{
		Name: "job-generation", Namespace: "project-1", Image: "busybox:1.36",
		Resources: map[string]int64{"vendor.example/gpu": 1},
		Annotations: map[string]string{"ai.compute/generation": "7"},
	}
	if err := client.ApplyJob(ctx, base); err != nil {
		t.Fatal(err)
	}
	next := base
	next.Annotations = map[string]string{"ai.compute/generation": "8"}
	if err := client.ApplyJob(ctx, next); err == nil {
		t.Fatal("new desired generation must not silently reuse an immutable older Job")
	}
	got, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, base.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations["ai.compute/generation"] != "7" {
		t.Fatalf("generation mismatch mutated existing execution object: %#v", got.Annotations)
	}
}


func TestProviderRecoversAfterApplyWithoutObservationByReplayingSameJob(t *testing.T) {
	ctx := context.Background()
	coreClient := kubefake.NewSimpleClientset()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{workloadGVR: "WorkloadList"},
	)
	provider := NewProvider(NewKubeClient(coreClient, dynamicClient))
	projection := baseprovider.WorkloadProjection{
		WorkloadID: "train-crash", PoolID: "pool-1", ClusterID: "cluster-a",
		Namespace: "project-1", Generation: 11, Image: "busybox:1.36",
		Accelerator: domain.AcceleratorRequest{Class: "h100", Quota: 1},
		AcceleratorBinding: domain.AcceleratorBinding{
			Class: "h100", ResourceName: "vendor.example/gpu", Flavor: "h100",
		},
	}

	// Simulate worker A reaching the provider side effect and then dying before
	// it can publish the observation to the control plane.
	job, err := ProjectWorkload(projection)
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.client.ApplyJob(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Worker B replays the full provider reconcile. It must reuse the same Job,
	// independently observe it, and return the desired generation.
	got, err := provider.ReconcileWorkload(ctx, projection)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := coreClient.BatchV1().Jobs("project-1").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("crash recovery duplicated provider side effects: jobs=%d", len(jobs.Items))
	}
	if got.ObservedGeneration != 11 {
		t.Fatalf("recovered generation=%d, want 11", got.ObservedGeneration)
	}
	if jobs.Items[0].Annotations["ai.compute/generation"] != "11" {
		t.Fatalf("provider identity lost generation fence: %#v", jobs.Items[0].Annotations)
	}
}
