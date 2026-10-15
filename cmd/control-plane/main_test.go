package main

import (
	"testing"

	"github.com/avaargsh/gpu-compute-platform/internal/platform/bootstrap"
)

func TestPortalIntegrationRetainsSafeDefaultListener(t *testing.T) {
	cfg, err := bootstrap.Load(func(string) string { return "" })
	if err != nil || cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("unsafe default listener: %#v %v", cfg, err)
	}
	if _, err := bootstrap.Load(func(key string) string {
		if key == "CONTROL_PLANE_LISTEN_ADDR" {
			return "0.0.0.0:8080"
		}
		return ""
	}); err == nil {
		t.Fatal("anonymous API accepted a public listener")
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
