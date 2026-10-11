package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestManagementCtlRejectsUnsafeIDsAndMissingReplayKey(t *testing.T) {
	base := options{Action: "put", Cluster: "cluster-a", Project: "research", Kind: "ComputePool", Resource: "pool-a", File: "pool.json", OperationID: "0123456789abcdef0123456789abcdef"}
	if err := validate(base); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*options)
	}{
		{"missing operation key", func(o *options) { o.OperationID = "" }},
		{"short operation key", func(o *options) { o.OperationID = "abcd" }},
		{"unsafe cluster", func(o *options) { o.Cluster = "../foreign" }},
		{"unsafe resource", func(o *options) { o.Resource = "pool/a" }},
		{"fake resource kind", func(o *options) { o.Kind = "Secret" }},
		{"no JSON input", func(o *options) { o.File = "" }},
		{"wrong action", func(o *options) { o.Action = "admin" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.change(&candidate)
			if err := validate(candidate); err == nil {
				t.Fatalf("invalid options accepted: %+v", candidate)
			}
		})
	}
}

func TestManagementCtlDeleteAndReceiptRequireExactIdentifiers(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	receipt := options{Action: "receipt", Cluster: "cluster-a", Project: "research", OperationID: key}
	if err := validate(receipt); err != nil {
		t.Fatal(err)
	}
	if got := endpoint(receipt); got != baseURL+"/api/v1/management/clusters/cluster-a/projects/research/operations/"+key {
		t.Fatalf("unexpected receipt URL: %s", got)
	}
	deletion := options{Action: "delete", Cluster: "cluster-a", Project: "research", Kind: "Workload", Resource: "job-1", OperationID: key}
	if err := validate(deletion); err == nil {
		t.Fatal("DELETE accepted without If-Match Generation")
	}
	deletion.Generation = 2
	if err := validate(deletion); err != nil {
		t.Fatal(err)
	}
	if got := endpoint(deletion); !strings.HasSuffix(got, "/resources/Workload/job-1") {
		t.Fatalf("unexpected resource endpoint: %s", got)
	}
}

func TestManagementCtlNewKeyWorksWithoutOIDCToken(t *testing.T) {
	t.Setenv("MANAGEMENT_ACCESS_TOKEN", "")
	var stdout, stderr bytes.Buffer
	if err := run(options{Action: "new-key"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}\n$`).MatchString(stdout.String()) {
		t.Fatalf("unexpected key format: %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("secret was printed to error output: %q", stderr.String())
	}
	var stdout2 bytes.Buffer
	if err := run(options{Action: "new-key"}, &stdout2, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() == stdout2.String() {
		t.Fatal("separate operations reused a random key")
	}
	if err := run(options{Action: "receipt", Cluster: "c", Project: "p", OperationID: strings.TrimSpace(stdout.String())}, &stdout, &stderr); err == nil {
		t.Fatal("receipt unexpectedly worked without access token")
	}
}
