package inflect

import "testing"

func TestSingularSuffixRules(t *testing.T) {
	for in, want := range map[string]string{
		"categories": "category",
		"statuses":   "status",
		"boxes":      "box",
		"batches":    "batch",
		"wishes":     "wish",
		"classes":    "class",
		"users":      "user",
		"Users":      "User",
		"status":     "status",
		"address":    "address",
		"analysis":   "analysis",
		"people":     "people",
		"":           "",
	} {
		if got := Singular(in); got != want {
			t.Errorf("Singular(%q) = %q, want %q", in, got, want)
		}
	}
}
