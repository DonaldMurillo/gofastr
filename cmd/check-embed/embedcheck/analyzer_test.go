package embedcheck

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestAnalyzerAnalysistest runs the analyzer against every resolvable shape and
// the fixtures that must remain silent.
func TestAnalyzerAnalysistest(t *testing.T) {
	analysistest.Run(t, testdataDir(t), Analyzer,
		"./src/clean",
		"./src/bad",
		"./src/notembeddable",
		"./src/interfacecomponent",
		"./src/chained",
		"./src/inline",
		"./src/whitespace",
		"./src/falsepositives",
		"./src/unresolved",
		"./src/childaction",
	)
}

// TestCleanSurfaceNotFlagged: an embeddable surface rendering a component that
// registers an ordinary G.setState action must not trip the gate.
func TestCleanSurfaceNotFlagged(t *testing.T) {
	if got := loadFindings(t, "clean"); len(got) != 0 {
		t.Fatalf("expected 0 findings on clean, got %d: %+v", len(got), got)
	}
}

// TestNonEmbeddableServerActionNotFlagged: a component with a server action
// that is NOT on any embeddable surface must not trip the gate. The gate
// targets reachability from an embed.Surface, not server actions in general.
func TestNonEmbeddableServerActionNotFlagged(t *testing.T) {
	if got := loadFindings(t, "notembeddable"); len(got) != 0 {
		t.Fatalf("expected 0 findings on notembeddable, got %d: %+v", len(got), got)
	}
}

// TestServerActionOnEmbedFlagged: an embeddable surface rendering a component
// that registers G.serverAction must be flagged, naming the surface, the
// component and the action, and pointing at the island-RPC / form-POST /
// polling alternatives.
func TestServerActionOnEmbedFlagged(t *testing.T) {
	got := loadFindings(t, "bad")
	if len(got) != 1 {
		t.Fatalf("expected 1 finding on bad, got %d: %+v", len(got), got)
	}
	f := got[0]
	if f.Surface != "bad" {
		t.Errorf("finding.Surface = %q, want %q", f.Surface, "bad")
	}
	if f.Component != "serverActionComp" {
		t.Errorf("finding.Component = %q, want %q", f.Component, "serverActionComp")
	}
	if f.Action != "save" {
		t.Errorf("finding.Action = %q, want %q", f.Action, "save")
	}
	msg := f.Format()
	for _, want := range []string{
		`embed surface "bad"`,
		`renders component "serverActionComp"`,
		`registers a server action "save"`,
		"island RPC, a form POST, or polling",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("finding message missing %q\nfull message:\n%s", want, msg)
		}
	}
	t.Logf("verbatim finding message:\n%s", msg)
}

// childaction is the rendered-child shape: the root declares no action and the
// child it renders declares a server action. Guards the gate that followed only
// the surface's root component type.
func TestAnalyzerResolvesSupportedScreenShapes(t *testing.T) {
	for _, pkgName := range []string{"interfacecomponent", "chained", "inline", "childaction"} {
		t.Run(pkgName, func(t *testing.T) {
			if got := loadFindings(t, pkgName); len(got) != 1 {
				t.Fatalf("expected 1 finding on %s, got %d: %+v", pkgName, len(got), got)
			}
		})
	}
}

func TestAnalyzerFlagsWhitespaceServerActionCall(t *testing.T) {
	got := loadFindings(t, "whitespace")
	if len(got) != 1 {
		t.Fatalf("expected 1 finding on whitespace, got %d: %+v", len(got), got)
	}
	if msg := got[0].Format(); !strings.Contains(msg, "G.serverAction(") {
		t.Fatalf("finding must name the canonical spelling G.serverAction(:\n%s", msg)
	}
}

func TestExecutableServerActionCallAcceptsUnicodeWhitespace(t *testing.T) {
	if !executableServerActionCall("G.serverAction\u00a0(\"save\")") {
		t.Fatal("scanner missed a server action separated by Unicode whitespace")
	}
}

// A template literal's ${...} is code, and the action compiler's rewrite
// is lexical (uihost replaces every "G.serverAction(" in the body), so a
// call inside an interpolation becomes a live server action. The scanner
// skipped the whole template as a string, the CLI printed a green tick,
// and the boot walk, which matches lexically, refused the same tree.
// Plain template text stays text.
func TestExecutableServerActionCallReadsTemplateInterpolations(t *testing.T) {
	for _, src := range []string{
		"G.toast(`saved ${G.serverAction(\"save\", params)}`)",
		"`a ${ {x: 1}.x } ${ `inner ${G.serverAction(\"save\")}` }`",
		"`${'}'} ${G.serverAction (\"save\")}`",
	} {
		if !executableServerActionCall(src) {
			t.Errorf("scanner missed the server action inside a template interpolation: %s", src)
		}
	}
	for _, src := range []string{
		"`G.serverAction(\"save\")`",
		"`${\"G.serverAction(\"}`",
		"`$G.serverAction(`",
		"`${ /* G.serverAction( */ 1 }`",
	} {
		if executableServerActionCall(src) {
			t.Errorf("scanner flagged template text or a string inside an interpolation: %s", src)
		}
	}
}

func TestAnalyzerIgnoresNonClientJSAndDeadRegistrations(t *testing.T) {
	if got := loadFindings(t, "falsepositives"); len(got) != 0 {
		t.Fatalf("expected 0 findings on falsepositives, got %d: %+v", len(got), got)
	}
}

// A surface whose walk cannot be completed must produce a note, not silence.
// "found nothing" and "could not look" read identically otherwise, which is how
// the rendered-child hole survived a release.
func TestAnalyzerNotesUnfollowableChild(t *testing.T) {
	findings, notes := loadAll(t, "unresolved")
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings on unresolved, got %d: %+v", len(findings), findings)
	}
	if len(notes) == 0 {
		t.Fatal("an interface-typed child field produced no note — the gate gave up silently")
	}
	msg := notes[0].Format()
	for _, want := range []string{`embed surface "unresolved"`, "interface-typed", "boot walk"} {
		if !strings.Contains(msg, want) {
			t.Errorf("note missing %q\nfull note:\n%s", want, msg)
		}
	}
}

// A surface the walk follows end to end must produce NO note. A gate that
// warned about everything would be ignored, which is the same outcome as
// warning about nothing.
func TestAnalyzerSilentWhenFullyResolved(t *testing.T) {
	for _, pkgName := range []string{"clean", "bad", "chained", "inline", "childaction"} {
		if _, notes := loadAll(t, pkgName); len(notes) != 0 {
			t.Errorf("%s produced %d unresolved note(s) on a fully followable tree: %+v",
				pkgName, len(notes), notes)
		}
	}
}

// --- mutation checks ----------------------------------------------------
//
// Each registers its edits with mutation (fixtures_test.go) and reads the
// mutated fixture through mutatedFindings, which fails the test when an edit
// did not apply and logs each edit that did.

// cleanMutated turns the clean surface's setState action into a server action.
var cleanMutated = mutation("clean", replacement{
	from: "G.setState('count', 1)",
	to:   `G.serverAction(\"save\")`,
})

// TestMutationCleanBecomesFlagged: turning the clean surface's setState action
// into a server action must produce exactly one finding, proving the clean
// case passed because it had no server action, not because detection is inert.
func TestMutationCleanBecomesFlagged(t *testing.T) {
	if got := mutatedFindings(t, cleanMutated); len(got) != 1 {
		t.Fatalf("after mutation, expected 1 finding on clean, got %d: %+v", len(got), got)
	}
}

// badMutated removes the bad surface's server action.
var badMutated = mutation("bad", replacement{
	from: `G.serverAction("save")`,
	to:   "G.setState('saved', true)",
})

// TestMutationBadBecomesClean: removing the bad surface's server action must
// drop the finding count to zero, proving the finding was due to the
// G.serverAction token, not a side effect.
func TestMutationBadBecomesClean(t *testing.T) {
	if got := mutatedFindings(t, badMutated); len(got) != 0 {
		t.Fatalf("after mutation, expected 0 findings on bad, got %d: %+v", len(got), got)
	}
}

// notEmbeddableMutated puts the notembeddable component onto an embeddable
// surface. Two edits land the surface: the fembed import and the Surface
// declaration that replaces RegisterScreen.
var notEmbeddableMutated = mutation("notembeddable",
	replacement{
		from: `"github.com/DonaldMurillo/gofastr/core/render"
)`,
		to: `"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)`,
	},
	replacement{
		from: `func Register(a *app.App) {
	a.RegisterScreen(app.NewScreen("/internal", &internalComp{}), nil)
}`,
		to: `func Surfaces() []fembed.Surface {
	scr := app.NewScreen("/internal", &internalComp{})
	return []fembed.Surface{{
		Name:    "internal",
		Screen:  scr,
		Origins: []string{"https://acme.example"},
	}}
}`,
	},
)

// TestMutationNonEmbeddableBecomesFlagged: putting the notembeddable component
// (which already registers a server action) onto an embeddable surface must
// produce a finding. That proves reachability from an embed.Surface, not the
// action itself, is what the gate keys on.
func TestMutationNonEmbeddableBecomesFlagged(t *testing.T) {
	if got := mutatedFindings(t, notEmbeddableMutated); len(got) != 1 {
		t.Fatalf("after mutation, expected 1 finding on notembeddable, got %d: %+v", len(got), got)
	}
}

// resolvedShapesMutated removes the server action from every resolved shape.
var resolvedShapesMutated = []string{
	mutation("interfacecomponent", replacement{from: `G.serverAction("save")`, to: `G.setState("saved", true)`}),
	mutation("chained", replacement{from: `G.serverAction("save")`, to: `G.setState("saved", true)`}),
	mutation("inline", replacement{from: `G.serverAction("save")`, to: `G.setState("saved", true)`}),
	mutation("childaction", replacement{from: `G.serverAction("save")`, to: `G.setState("saved", true)`}),
	mutation("whitespace", replacement{from: `G.serverAction ("save")`, to: `G.setState("saved", true)`}),
}

func TestMutationResolvedShapesBecomeClean(t *testing.T) {
	for _, pkgName := range resolvedShapesMutated {
		t.Run(pkgName, func(t *testing.T) {
			if got := mutatedFindings(t, pkgName); len(got) != 0 {
				t.Fatalf("after mutation, expected 0 findings on %s, got %d: %+v",
					pkgName, len(got), got)
			}
		})
	}
}
