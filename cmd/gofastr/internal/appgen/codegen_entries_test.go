package appgen

import "testing"

func TestYAMLScalarQuotesWhatYAMLReadsAsAnotherType(t *testing.T) {
	for _, v := range []string{"null", "Null", "true", "no", "y", "*", "", "1x", "a b", "a:b"} {
		if got := yamlScalar(v); got == v {
			t.Errorf("yamlScalar(%q) left it plain", v)
		}
	}
	for _, v := range []string{"mycli", "gen/cli", "api_v2", "a.b-c"} {
		if got := yamlScalar(v); got != v {
			t.Errorf("yamlScalar(%q) = %s, want it plain", v, got)
		}
	}
}
