package embedcheck

import "testing"

// The boot-time reachability walk in framework/uihost/embed_actions.go cannot
// see a child a component BUILDS in Render(). The child does not exist as a
// value until Render runs (pinned by TestBootWalkCannotSeeRenderBuiltChild).
// The analyzer is the gate that covers that shape, so for a render-built child
// it must produce EITHER a finding OR an unresolved note. Silence means both
// gates are quiet and the child's server action ships into the frame.
func TestSeesRenderBuiltChild(t *testing.T) {
	findings, notes := loadAll(t, "renderbuilt")
	if len(findings) == 0 && len(notes) == 0 {
		t.Fatal("analyzer reported neither a finding nor an unresolved note for an embeddable root " +
			"that constructs an action-bearing child inside Render(); combined with the boot walk's " +
			"blindness to the same shape, the child's server action ships to the frame with no warning")
	}
	t.Logf("findings=%d notes=%d", len(findings), len(notes))
	for _, f := range findings {
		t.Logf("finding: %+v", f)
	}
	for _, n := range notes {
		t.Logf("note: %+v", n)
	}
}

// The cross-package spelling of the same shape: the child is BUILT in Render()
// AND lives in another package, the two conditions 6ed8667a names as the
// boot walk's blind spot and static analysis's give-up point respectively.
// Silence from both means an action-bearing component ships into the frame
// with no warning at build time and no panic at boot.
func TestNotesCrossPackageRenderBuiltChild(t *testing.T) {
	findings, notes := loadAll(t, "renderbuiltxpkg")
	t.Logf("findings=%d notes=%d", len(findings), len(notes))
	for _, f := range findings {
		t.Logf("finding: %+v", f)
	}
	for _, n := range notes {
		t.Logf("note: %+v", n)
	}
	if len(findings) == 0 && len(notes) == 0 {
		t.Fatal("analyzer reported neither a finding nor an unresolved note for an embeddable root " +
			"that constructs an action-bearing child from ANOTHER PACKAGE inside Render(); the boot " +
			"walk is blind to the same shape, so nothing warns and the action ships to the frame")
	}
}

// The island shape: the root HOLDS the child, but inside an island.Island.
// The boot walk descends into island wrappers (they are composition, not a
// host back-reference), and the analyzer covers it too, belt and braces for
// the framework's main composition primitive.
func TestSeesIslandWrappedChild(t *testing.T) {
	findings, notes := loadAll(t, "islandchild")
	t.Logf("findings=%d notes=%d", len(findings), len(notes))
	for _, f := range findings {
		t.Logf("finding: %+v", f)
	}
	for _, n := range notes {
		t.Logf("note: %+v", n)
	}
	if len(findings) == 0 && len(notes) == 0 {
		t.Fatal("analyzer reported neither a finding nor an unresolved note for an embeddable root " +
			"holding an action-bearing child inside an island.Island; the boot walk stops at " +
			"core-ui/island, so BOTH gates are silent and the action ships to the frame")
	}
}

// The decisive combination: an island wrapper AND a child type from another
// package. The analyzer cannot produce a FINDING here; it has no syntax tree
// for the child's Actions(), so the only thing that can stop this shipping is
// the note, which now fails `gofastr build`.
//
// This is the case that justifies making notes fatal. Before, the analyzer
// emitted a note, the build ignored it, and the boot walk was cited as the
// covering gate, but the boot walk reads built VALUES and cannot see a child
// constructed inside Render(). Both were quiet at once.
func TestNotesIslandCrossPackageChild(t *testing.T) {
	findings, notes := loadAll(t, "islandxpkg")
	t.Logf("findings=%d notes=%d", len(findings), len(notes))
	for _, n := range notes {
		t.Logf("note: %+v", n)
	}
	if len(findings) == 0 && len(notes) == 0 {
		t.Fatal("analyzer was silent for an island-wrapped, cross-package action-bearing " +
			"child — neither a finding nor a note, so nothing stops it reaching a customer's frame")
	}
}

// Only one note class stops a build: a child constructed inside Render() whose
// type is in another package, which neither this analyzer nor the boot walk
// can see. Everything else is advisory, because the boot walk reads live
// values and covers it.
//
// Failing on every note was tried and reverted. It rejected clean island
// surfaces, interface-typed fields the analyzer had already resolved, and the
// fixture named for false positives, with no remedy available.
func TestOnlyRenderBuiltCrossPackageBlocks(t *testing.T) {
	cases := []struct {
		fixture string
		want    bool // expect at least one BLOCKING note
	}{
		{"renderbuiltxpkg", true}, // neither gate can see it
		{"renderbuilt", false},    // same package: analyzer resolves it
		{"islandchild", false},    // wrapper: boot walk descends through it
		{"islandxpkg", false},     // ditto, child supplied as a value
		{"cleanisland", false},    // fully resolved, nothing to warn about
		{"unresolved", false},     // interface field: boot walk sees the value
		{"falsepositives", false}, // named for shapes that must not fail
	}
	for _, tc := range cases {
		_, notes := loadAll(t, tc.fixture)
		var blocking int
		for _, n := range notes {
			if n.Blocking {
				blocking++
			}
		}
		if got := blocking > 0; got != tc.want {
			t.Errorf("%s: blocking=%d (want any=%v); notes=%d", tc.fixture, blocking, tc.want, len(notes))
			for _, n := range notes {
				t.Logf("  blocking=%v %s", n.Blocking, n.Reason)
			}
		}
	}
}
