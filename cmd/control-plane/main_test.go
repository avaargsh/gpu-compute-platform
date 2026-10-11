package main

import "testing"

func TestListenAddressDefaultsToExistingServerBinding(t *testing.T) {
	t.Setenv("CONTROL_PLANE_LISTEN_ADDR", "")
	if got := listenAddress(); got != ":8080" {
		t.Fatalf("listenAddress()=%q, want :8080", got)
	}
}

func TestListenAddressAllowsLocalPortalIsolation(t *testing.T) {
	t.Setenv("CONTROL_PLANE_LISTEN_ADDR", "127.0.0.1:8080")
	if got := listenAddress(); got != "127.0.0.1:8080" {
		t.Fatalf("listenAddress()=%q, want 127.0.0.1:8080", got)
	}
}

func TestOperatorPreviewRequiresLoopback(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.50:8080", "example.com:8080", ""} {
		if isLoopbackAddress(addr) {
			t.Fatalf("unsafe operator preview bind accepted: %q", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8080", "[::1]:8080"} {
		if !isLoopbackAddress(addr) {
			t.Fatalf("safe loopback bind rejected: %q", addr)
		}
	}
}

func TestOperatorPreviewConfiguredFailsClosedWhenOnlyOneEnvFieldSet(t *testing.T) {
	t.Setenv("PORTAL_OPERATOR_TOKEN", "")
	t.Setenv("PORTAL_OPERATOR_CLUSTER_ID", "")
	t.Setenv("PORTAL_OPERATOR_PROJECT_IDS", "")
	if operatorPreviewConfigured() {
		t.Fatal("disabled preview treated as enabled")
	}
	t.Setenv("PORTAL_OPERATOR_PROJECT_IDS", "research")
	if !operatorPreviewConfigured() {
		t.Fatal("incomplete preview configuration was ignored")
	}
}
