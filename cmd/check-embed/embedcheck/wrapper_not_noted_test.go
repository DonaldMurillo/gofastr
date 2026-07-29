package embedcheck

import "testing"

// ROUND-2 REVIEW REPRO.
//
// Making Unresolved fatal (cmd/gofastr/build.go buildEmbedGate) turned two
// pre-existing note shapes into build failures:
//
//  1. a component-typed value from another package appearing anywhere in a
//     Render body — including island.Island, the framework's own composition
//     primitive, which componentsInRenderBodies now notes; and
//  2. an interface-typed component field, which walkComponentType has always
//     noted.
//
// Both fire on surfaces that are fully analysable and provably clean, and
// neither has a fix the note's own message suggests (you cannot "move
// island.Island into this package").
//
// cleanisland is the exact shape the blueprint emits for an island block,
// with an action-free same-package child. The analyzer resolves the child
// completely and finds nothing. It must therefore be SILENT.
func TestCleanIslandSurfaceIsNotNoted(t *testing.T) {
	findings, notes := loadAll(t, "cleanisland")
	for _, f := range findings {
		t.Logf("finding: %+v", f)
	}
	for _, n := range notes {
		t.Logf("note: %s", n.Format())
	}
	if len(findings) != 0 {
		t.Fatalf("clean island surface produced %d finding(s)", len(findings))
	}
	if len(notes) != 0 {
		t.Fatalf("clean, fully-resolved island surface produced %d unresolved note(s); "+
			"since a note now fails `gofastr build`, every app that renders an island "+
			"inside an embed surface can no longer build", len(notes))
	}
}

// The interfacecomponent fixture is an existing, deliberate, CLEAN-resolution
// shape: the concrete type behind the interface IS followed and its action IS
// found. Any note it also emits is now fatal, so an app with an
// interface-typed component field cannot build even when the analyzer proved
// exactly what it holds.
func TestInterfaceFieldNoteIsNotFatalMaterial(t *testing.T) {
	_, notes := loadAll(t, "interfacecomponent")
	for _, n := range notes {
		t.Logf("note: %s", n.Format())
	}
	if len(notes) != 0 {
		t.Fatalf("interface-typed component surface produced %d note(s), each of which "+
			"now fails the build", len(notes))
	}
}
