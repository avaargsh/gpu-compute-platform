package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"reflect"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// The Namespace's app.kubernetes.io/component label is editable. A dispatcher
// must not bypass the Observer Job envelope by removing that label. Use the
// API-server-managed kubernetes.io/metadata.name label for BOTH match selectors.
func TestObserverJobEnvelopeNamespaceScopeCannotBeDisabledByAppLabel(t *testing.T) {
	const path = "../../deploy/kubernetes/verifiedops-observer/observer-job-policy.yaml"
	const label = "kubernetes.io/metadata.name"
	want := map[string]string{label: "verifiedops-observer"}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	var sawPolicy, sawBinding bool
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var header struct {
			Kind string `json:"kind"`
		}
		if err := yaml.Unmarshal(doc, &header); err != nil {
			t.Fatal(err)
		}
		switch header.Kind {
		case "ValidatingAdmissionPolicy":
			var policy admissionregistrationv1.ValidatingAdmissionPolicy
			if err := yaml.Unmarshal(doc, &policy); err != nil {
				t.Fatal(err)
			}
			if policy.Name != "verifiedops-observer-job-envelope" {
				continue
			}
			sawPolicy = true
			assertObserverNamespaceMatch(t, "policy", policy.Spec.MatchConstraints.NamespaceSelector, want)
		case "ValidatingAdmissionPolicyBinding":
			var binding admissionregistrationv1.ValidatingAdmissionPolicyBinding
			if err := yaml.Unmarshal(doc, &binding); err != nil {
				t.Fatal(err)
			}
			if binding.Spec.PolicyName != "verifiedops-observer-job-envelope" {
				continue
			}
			sawBinding = true
			if binding.Spec.MatchResources == nil {
				t.Fatal("Observer Job binding has no namespace match constraints")
			}
			assertObserverNamespaceMatch(t, "binding", binding.Spec.MatchResources.NamespaceSelector, want)
			if len(binding.Spec.ValidationActions) != 1 || binding.Spec.ValidationActions[0] != admissionregistrationv1.Deny {
				t.Fatalf("Observer Job binding must deny noncompliant Jobs: %v", binding.Spec.ValidationActions)
			}
		}
	}
	if !sawPolicy || !sawBinding {
		t.Fatalf("expected both Observer Job policy and bound Deny action, got policy=%v binding=%v", sawPolicy, sawBinding)
	}
}

func assertObserverNamespaceMatch(t *testing.T, source string, selector *metav1.LabelSelector, expected map[string]string) {
	t.Helper()
	if selector == nil || !reflect.DeepEqual(selector.MatchLabels, expected) || len(selector.MatchExpressions) != 0 {
		t.Fatalf("%s must use only the API-server-managed namespace-name label; got %+v, want %v", source, selector, expected)
	}
}
