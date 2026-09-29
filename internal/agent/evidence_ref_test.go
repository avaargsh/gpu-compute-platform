package agent

import "testing"

func TestAIFactoryEvidenceRefRoundTrip(t *testing.T) {
	ref, err := AIFactoryEvidenceRef("bundle-train-1-gen-7")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "aifactory://evidence/bundle-train-1-gen-7" {
		t.Fatalf("ref=%q", ref)
	}
	bundleID, err := ParseAIFactoryEvidenceRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	if bundleID != "bundle-train-1-gen-7" {
		t.Fatalf("bundleID=%q", bundleID)
	}
}

func TestAIFactoryEvidenceRefRejectsInvalidBoundary(t *testing.T) {
	for _, value := range []string{"", "a/b"} {
		if _, err := AIFactoryEvidenceRef(value); err == nil {
			t.Fatalf("expected invalid bundle id %q", value)
		}
	}
	for _, ref := range []string{"https://example/evidence/bundle-1", "aifactory://other/bundle-1", "aifactory://evidence/"} {
		if _, err := ParseAIFactoryEvidenceRef(ref); err == nil {
			t.Fatalf("expected invalid ref %q", ref)
		}
	}
}
