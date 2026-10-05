package provider

import "testing"

func TestSupportedAdapterCatalog(t *testing.T) {
	got := SupportedAdapterNames()
	if len(got) != 1 || got[0] != KueueAdapterName {
		t.Fatalf("supported adapters=%v, want [%s]", got, KueueAdapterName)
	}
	if !IsSupportedAdapter(KueueAdapterName) {
		t.Fatalf("%s must be supported", KueueAdapterName)
	}
	for _, name := range []string{"", "volcano", "kai", "dra", "hami"} {
		if IsSupportedAdapter(name) {
			t.Fatalf("unpromoted adapter %q must remain unsupported", name)
		}
	}
}
