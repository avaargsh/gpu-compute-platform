package volcano

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
	// A DELETE/UPDATE resourceVersion conflict is retryable only by re-entering
	// reconciliation from a fresh GET. Never retry the stale mutation directly:
	// the next attempt must re-check ownership, generation and immutable spec.
	if apierrors.IsConflict(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) ||
		errors.Is(err, context.DeadlineExceeded) {
		return baseprovider.MarkRetryable(err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return baseprovider.MarkRetryable(err)
	}
	return err
}
