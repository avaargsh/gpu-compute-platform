package kueue

import (
	"context"
	"errors"
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	baseprovider "github.com/avaargsh/gpu-compute-platform/internal/provider"
)

func classifyProviderError(err error) error {
	if err == nil {
		return nil
	}
	if isTransientKubernetesError(err) {
		return baseprovider.MarkRetryable(err)
	}
	return err
}

func isTransientKubernetesError(err error) bool {
	if apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
