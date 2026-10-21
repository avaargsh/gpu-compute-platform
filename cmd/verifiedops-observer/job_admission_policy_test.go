package main

import (
	"bufio"
	"io"
	"os"
	"sort"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const (
	observerJobPolicyPath = "../../deploy/kubernetes/verifiedops-observer/observer-job-policy.yaml"
	observerGuardPath     = "../../deploy/kubernetes/verifiedops-observer/observer-admission-policy.example.yaml"
	observerJobExample    = "../../deploy/kubernetes/verifiedops-observer/observer-job.example.yaml"
)

// The one-shot contract of an Observer Job. Every field is API-defaulted to
// a single-execution shape (completions=1, parallelism=1,
// completionMode=NonIndexed, manualSelector=false), so the admission
// constraint must accept both the defaulted and the explicit spelling.
var oneShotConjuncts = []string{
	"(!has(object.spec.completions) || object.spec.completions == 1)",
	"(!has(object.spec.parallelism) || object.spec.parallelism == 1)",
	"(!has(object.spec.completionMode) || object.spec.completionMode == 'NonIndexed')",
	"(!has(object.spec.manualSelector) || object.spec.manualSelector == false)",
	"object.spec.template.spec.restartPolicy == 'Never'",
}

func readAdmissionPolicies(t *testing.T, path string) []admissionregistrationv1.ValidatingAdmissionPolicy {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var policies []admissionregistrationv1.ValidatingAdmissionPolicy
	reader := utilyaml.NewYAMLReader(bufio.NewReader(file))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(strings.TrimSpace(string(doc))) == 0 {
			continue
		}
		var policy admissionregistrationv1.ValidatingAdmissionPolicy
		if err := yaml.Unmarshal(doc, &policy); err != nil {
			// Documents that are not a ValidatingAdmissionPolicy (bindings,
			// other resource kinds) are not part of this contract.
			continue
		}
		if policy.Kind != "ValidatingAdmissionPolicy" || policy.Name == "" {
			continue
		}
		policies = append(policies, policy)
	}
	if len(policies) == 0 {
		t.Fatalf("%s declares no ValidatingAdmissionPolicy", path)
	}
	return policies
}

// findPolicy returns the named policy and asserts it covers batch/v1 Jobs on
// both CREATE and UPDATE, which is where the one-shot shape is decided.
func findPolicy(t *testing.T, policies []admissionregistrationv1.ValidatingAdmissionPolicy, name string) admissionregistrationv1.ValidatingAdmissionPolicy {
	t.Helper()
	for _, policy := range policies {
		if policy.Name != name {
			continue
		}
		var coversJobCreate, coversJobUpdate bool
		for _, rule := range policy.Spec.MatchConstraints.ResourceRules {
			if len(rule.APIGroups) == 0 || rule.APIGroups[0] != "batch" {
				continue
			}
			if len(rule.APIVersions) == 0 || rule.APIVersions[0] != "v1" {
				continue
			}
			if len(rule.Resources) == 0 || rule.Resources[0] != "jobs" {
				continue
			}
			for _, op := range rule.Operations {
				switch op {
				case admissionregistrationv1.Create:
					coversJobCreate = true
				case admissionregistrationv1.Update:
					coversJobUpdate = true
				}
			}
		}
		if !coversJobCreate || !coversJobUpdate {
			t.Fatalf("policy %s must match batch/v1 jobs on CREATE and UPDATE (create=%v update=%v)",
				name, coversJobCreate, coversJobUpdate)
		}
		return policy
	}
	t.Fatalf("policy %s not found", name)
	return admissionregistrationv1.ValidatingAdmissionPolicy{}
}

// assertOneShotClause requires the policy to carry exactly the documented
// single-execution clause: the same set of conjuncts, no weaker and no extra
// hidden condition.
func assertOneShotClause(t *testing.T, policy admissionregistrationv1.ValidatingAdmissionPolicy, message string) {
	t.Helper()
	var matched *admissionregistrationv1.Validation
	for i := range policy.Spec.Validations {
		if policy.Spec.Validations[i].Message == message {
			matched = &policy.Spec.Validations[i]
			break
		}
	}
	if matched == nil {
		t.Fatalf("policy %s has no validation with message %q", policy.Name, message)
	}
	got := strings.ReplaceAll(strings.Join(strings.Fields(matched.Expression), " "), " ", " ")
	got = strings.ReplaceAll(got, `"`, "'")
	got = strings.ReplaceAll(got, "&&", "&& ")
	parts := strings.Split(got, "&&")
	gotConjuncts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		gotConjuncts = append(gotConjuncts, part)
	}
	want := append([]string(nil), oneShotConjuncts...)
	sort.Strings(want)
	sort.Strings(gotConjuncts)
	if len(want) != len(gotConjuncts) {
		t.Fatalf("policy %s one-shot clause must be exactly %v, got %v", policy.Name, want, gotConjuncts)
	}
	for i := range want {
		if want[i] != gotConjuncts[i] {
			t.Fatalf("policy %s one-shot clause must be exactly %v, got %v", policy.Name, want, gotConjuncts)
		}
	}
}

func TestObserverJobAdmissionPoliciesEnforceSingleExecutionShape(t *testing.T) {
	envelope := findPolicy(t, readAdmissionPolicies(t, observerJobPolicyPath), "verifiedops-observer-job-envelope")
	assertOneShotClause(t, envelope,
		"Observer Jobs must declare a one-shot configuration with completions=1, parallelism=1, NonIndexed completion mode, no manual selector, and a Never restart policy")

	guard := findPolicy(t, readAdmissionPolicies(t, observerGuardPath), "verifiedops-observer-job-guard")
	assertOneShotClause(t, guard,
		"VerifiedOps Job must be single-execution: completions=1, parallelism=1, NonIndexed completion mode, no manual selector, and a Never restart policy")
}

func TestObserverJobExampleIsExplicitlyOneShot(t *testing.T) {
	content, err := os.ReadFile(observerJobExample)
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := yaml.UnmarshalStrict(content, &job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatal("example Job must stay suspended")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatal("example Job must not retry")
	}
	if job.Spec.Completions == nil || *job.Spec.Completions != 1 {
		t.Fatal("example Job must declare a single completion")
	}
	if job.Spec.Parallelism == nil || *job.Spec.Parallelism != 1 {
		t.Fatal("example Job must declare a single parallel Pod")
	}
	if job.Spec.CompletionMode == nil || *job.Spec.CompletionMode != batchv1.NonIndexedCompletion {
		t.Fatal("example Job must use the NonIndexed completion mode")
	}
	if job.Spec.ManualSelector != nil && *job.Spec.ManualSelector {
		t.Fatal("example Job must not own its selector")
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatal("example Job must not allow the Observer process to restart in place")
	}
}
