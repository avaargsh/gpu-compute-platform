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
	if apierrors.IsTimeout(err) ||
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
