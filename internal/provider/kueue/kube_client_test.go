package kueue

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestKubeClientPoolAndWorkloadGoldenPath(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	dynamicClient := fake.NewSimpleDynamicClient(scheme)
	coreClient := kubefake.NewSimpleClientset()
	client := NewKubeClient(coreClient, dynamicClient)

	if err := client.ApplyResourceFlavor(ctx, ResourceFlavor{
		Name:         "accel-h100-80g",
		ResourceName: gpuResourceName,
		NodeLabels:   map[string]string{"ai.compute/accelerator-class": "h100-80g"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyClusterQueue(ctx, ClusterQueue{
		Name: "cq-pool-h100",
		Quotas: []ResourceQuota{
			{Flavor: "accel-h100-80g", Resource: gpuResourceName, Nominal: 8},
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
		Resources: map[string]int64{gpuResourceName: 2},
		Annotations: map[string]string{"kueue.x-k8s.io/queue-name": "lq-pool-h100"},
	}); err != nil {
		t.Fatal(err)
	}

	job, err := coreClient.BatchV1().Jobs("project-1").Get(ctx, "job-train-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Active = 1
	if _, err := coreClient.BatchV1().Jobs("project-1").UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
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
	if !observed.Admitted || !observed.PodsReady || observed.Phase != "Running" {
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

