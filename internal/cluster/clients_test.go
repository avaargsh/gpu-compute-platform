package cluster

import (
	"testing"

	"k8s.io/client-go/rest"
)

func TestNewClientsRejectsNilConfig(t *testing.T) {
	if _, err := NewClients(nil); err == nil {
		t.Fatal("expected nil config to fail")
	}
}

func TestNewClientsBuildsClientsets(t *testing.T) {
	clients, err := NewClients(&rest.Config{Host: "https://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if clients.Core == nil || clients.Dynamic == nil {
		t.Fatal("expected typed and dynamic clients")
	}
}
