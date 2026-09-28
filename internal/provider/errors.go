package provider

import (
	"errors"
	"fmt"
)

// Retryable marks a provider failure as transient. Providers classify the
// failure; the agent owns scheduling and backoff.
type Retryable struct {
	Err error
}

func (e Retryable) Error() string {
	if e.Err == nil {
		return "retryable provider error"
	}
	return fmt.Sprintf("retryable provider error: %v", e.Err)
}

func (e Retryable) Unwrap() error { return e.Err }

func MarkRetryable(err error) error {
	if err == nil {
		return nil
	}
	return Retryable{Err: err}
}

func IsRetryable(err error) bool {
	var target Retryable
	return errors.As(err, &target)
}
