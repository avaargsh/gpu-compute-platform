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
