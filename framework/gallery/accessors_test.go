package gallery

import "testing"

// TestAccessorsReturnRegisteredValues pins that the private codeSnippets
// and noteOnlySlugs maps are readable through their accessors
// (CodeSnippet, IsNoteOnly) and return the same values the old exported
// maps did. Before the fix, request handlers read the raw exported maps
// directly — a host mutating one after serving began would fatal-crash
// on concurrent map access. The maps are now unexported; these accessors
// are the only read path.
func TestAccessorsReturnRegisteredValues(t *testing.T) {
	// A slug known to have a code snippet (see catalog.go codeSnippets).
	if code := CodeSnippet("metricband"); code == "" {
		t.Error(`CodeSnippet("metricband") returned empty — expected the registered example Go`)
	}
	// An unknown slug must return "" (zero value), not panic.
	if code := CodeSnippet("no-such-slug"); code != "" {
		t.Errorf(`CodeSnippet("no-such-slug") = %q, want ""`, code)
	}

	// A slug known to be note-only (see catalog.go noteOnlySlugs).
	if !IsNoteOnly("datatable") {
		t.Error(`IsNoteOnly("datatable") = false, want true`)
	}
	// An unknown slug must return false (zero value), not panic.
	if IsNoteOnly("no-such-slug") {
		t.Error(`IsNoteOnly("no-such-slug") = true, want false`)
	}
}
