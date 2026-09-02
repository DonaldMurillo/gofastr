package stability

import "testing"

// TestClassifyRequiresSegmentBoundary pins that a manifest rule matches
// only at a path-segment boundary. Classify accepts a rule when
// strings.HasPrefix(rel, r.prefix) is true, with no boundary check, so a
// new top-level tree whose name merely STARTS with a classified prefix —
// cmdline under cmd, frameworks under framework, corev9 under core —
// inherits that tier and the coverage gate never fires. That contradicts
// the manifest's own contract (stability.go: "a newly added top-level
// tree fails the gate until it is classified on purpose") and fails the
// gate open: the honest signal for adding a tree silently disappears
// exactly when the new tree looks most like an existing one.
func TestClassifyRequiresSegmentBoundary(t *testing.T) {
	siblings := []string{
		"cmdline",    // shares prefix with {"cmd", Provisional}
		"kiln2",      // shares prefix with {"kiln", Experimental}
		"corev9",     // shares prefix with {"core", Provisional}
		"frameworks", // shares prefix with {"framework", Provisional}
		"sqlite3",    // shares prefix with {"sqlite", Provisional}
		"batteries",  // shares prefix with {"battery", Provisional}
	}
	for _, rel := range siblings {
		if tier, ok := Classify(ModulePath + "/" + rel); ok {
			t.Errorf("Classify(%q) = %q by prefix match; a sibling-named top-level tree must stay unclassified until the manifest names it", rel, tier)
		}
	}
	// Boundary control: the same prefixes must keep classifying real
	// subtrees and their exact roots, so the fix is a boundary check, not
	// a weaker prefix table.
	for _, rel := range []string{"cmd", "cmd/gofastr", "framework/crud", "kiln", "core"} {
		if _, ok := Classify(ModulePath + "/" + rel); !ok {
			t.Errorf("Classify(%q) = false; segment-boundary paths must still match their rule", rel)
		}
	}
}
