package kueue

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

// This only hardens read-only scheduler-condition attribution. It does not
// convert an adopted, same-generation Job into operation CREATE ownership.
func TestKueueWorkloadOwnerRequiresExactJobControllerReference(t *testing.T) {
	yes, no := true, false
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-a", UID: "uid-a"}}
	valid := metav1.OwnerReference{
		APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "uid-a", Controller: &yes,
	}
	foreign := metav1.OwnerReference{
		APIVersion: "batch/v1", Kind: "Job", Name: "job-b", UID: "uid-b", Controller: &yes,
	}
	nonController := metav1.OwnerReference{
		APIVersion: "v1", Kind: "ConfigMap", Name: "source", UID: "owner-uid", Controller: &no,
	}
	cases := []struct {
		name string
		job  *batchv1.Job
		refs []metav1.OwnerReference
		want bool
	}{
		{name: "exact-controller", job: job, refs: []metav1.OwnerReference{valid}, want: true},
		{name: "no-owner", job: job, refs: nil},
		{name: "nil-job", job: nil, refs: []metav1.OwnerReference{valid}},
		{name: "missing-job-uid", job: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job-a"}}, refs: []metav1.OwnerReference{valid}},
		{name: "missing-job-name", job: &batchv1.Job{ObjectMeta: metav1.ObjectMeta{UID: "uid-a"}}, refs: []metav1.OwnerReference{valid}},
		{name: "different-api-version", job: job, refs: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Job", Name: "job-a", UID: "uid-a", Controller: &yes}}},
		{name: "different-kind", job: job, refs: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: "job-a", UID: "uid-a", Controller: &yes}}},
		{name: "different-job-name", job: job, refs: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-b", UID: "uid-a", Controller: &yes}}},
		{name: "different-job-uid", job: job, refs: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "uid-b", Controller: &yes}}},
		{name: "missing-controller", job: job, refs: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "uid-a"}}},
		{name: "false-controller", job: job, refs: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "uid-a", Controller: &no}}},
		{name: "multiple-controller-references", job: job, refs: []metav1.OwnerReference{foreign, valid}},
		{name: "additional-non-controller-owner", job: job, refs: []metav1.OwnerReference{nonController, valid}, want: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := ownedByJob(tt.refs, tt.job); got != tt.want {
				t.Fatalf("ownership attribution = %v, want %v", got, tt.want)
			}
		})
	}
}

// A forged or incomplete Workload ownerRef must not supply the quota/admission
// conditions of the current Job. The Job's UID alone is insufficient.
func TestObserveJobRejectsUnownedKueueAdmissionConditions(t *testing.T) {
	yes, no := true, false
	valid := metav1.OwnerReference{
		APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "job-uid-a", Controller: &yes,
	}
	tests := []struct {
		name     string
		owners   []metav1.OwnerReference
		accepted bool
	}{
		{name: "real controller", owners: []metav1.OwnerReference{valid}, accepted: true},
		{name: "spoofed different name", owners: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-b", UID: "job-uid-a", Controller: &yes}}},
		{name: "not controller", owners: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "job-uid-a", Controller: &no}}},
		{name: "no controller field", owners: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job-a", UID: "job-uid-a"}}},
		{name: "wrong api group", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "Job", Name: "job-a", UID: "job-uid-a", Controller: &yes}}},
		{name: "multiple controllers", owners: []metav1.OwnerReference{valid, {APIVersion: "batch/v1", Kind: "Job", Name: "job-b", UID: "job-uid-b", Controller: &yes}}},
		{name: "additional non-controller owner", owners: []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "source", UID: "owner-uid", Controller: &no}, valid}, accepted: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
				Name: "job-a", Namespace: "project-a", UID: "job-uid-a", ResourceVersion: "1",
			}}
			core := kubefake.NewSimpleClientset(job)
			dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
				runtime.NewScheme(), map[schema.GroupVersionResource]string{workloadGVR: "WorkloadList"},
			)
			workload := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "kueue.x-k8s.io/v1beta2",
				"kind":       "Workload",
				"metadata":   map[string]any{"name": "job-a-wl", "namespace": "project-a"},
				"status": map[string]any{"conditions": []any{
					map[string]any{"type": "QuotaReserved", "status": "True"},
					map[string]any{"type": "Admitted", "status": "True"},
				}},
			}}
			workload.SetOwnerReferences(tc.owners)
			if _, err := dynamic.Resource(workloadGVR).Namespace("project-a").Create(
				ctx, workload, metav1.CreateOptions{},
			); err != nil {
				t.Fatal(err)
			}
			got, err := NewKubeClient(core, dynamic).ObserveJob(ctx, "project-a", "job-a")
			if err != nil {
				t.Fatal(err)
			}
			if got.Admitted != tc.accepted || got.QuotaReserved != tc.accepted {
				t.Fatalf("admission attributed to foreign Workload: %+v, expected accepted=%v", got, tc.accepted)
			}
			if tc.accepted && got.WorkloadName != "job-a-wl" {
				t.Fatalf("valid Kueue Workload disappeared from evidence: %+v", got)
			}
			if !tc.accepted && got.WorkloadName != "" {
				t.Fatalf("unowned Workload should not be attributed: %+v", got)
			}
		})
	}
}
