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

func TestSingularWordsEndingInUse(t *testing.T) {
	for input, want := range map[string]string{"houses": "house", "causes": "cause", "abuses": "abuse", "statuses": "status", "buses": "bus", "viruses": "virus"} {
		if got := Singular(input); got != want {
			t.Errorf("Singular(%q) = %q, want %q", input, got, want)
		}
	}
}
