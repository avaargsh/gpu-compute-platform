package kueue

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func TestClassifyProviderErrorMarksOnlyTransientFailuresRetryable(t *testing.T) {
	resource := schema.GroupResource{Group: "batch", Resource: "jobs"}
	cases := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "timeout", err: apierrors.NewTimeoutError("timeout", 1), retryable: true},
		{name: "server timeout", err: apierrors.NewServerTimeout(resource, "get", 1), retryable: true},
		{name: "too many requests", err: apierrors.NewTooManyRequests("busy", 1), retryable: true},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("down"), retryable: true},
		{name: "internal error", err: apierrors.NewInternalError(errors.New("apiserver")), retryable: true},
		{name: "deadline", err: context.DeadlineExceeded, retryable: true},
		{name: "not found", err: apierrors.NewNotFound(resource, "job-1"), retryable: false},
		{name: "invalid spec", err: errors.New("invalid accelerator binding"), retryable: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyProviderError(tc.err)
			if baseprovider.IsRetryable(got) != tc.retryable {
				t.Fatalf("retryable=%t, want %t: %v", baseprovider.IsRetryable(got), tc.retryable, got)
			}
		})
	}
}
