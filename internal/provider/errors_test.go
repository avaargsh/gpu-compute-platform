package provider

import (
	"errors"
	"testing"
)

func TestRetryableErrorContract(t *testing.T) {
	base := errors.New("temporary api failure")
	err := MarkRetryable(base)
	if !IsRetryable(err) {
		t.Fatal("marked provider error must be retryable")
	}
	if !errors.Is(err, base) {
		t.Fatal("retryable error must preserve cause")
	}
	if IsRetryable(base) {
		t.Fatal("plain provider error must remain permanent")
	}
	if MarkRetryable(nil) != nil {
		t.Fatal("nil must remain nil")
	}
}
