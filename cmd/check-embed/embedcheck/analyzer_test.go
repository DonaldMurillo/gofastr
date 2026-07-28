package embedcheck

import (
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/packages"
)

// testdataDir is the absolute path to the testdata module. go test runs this
// test with the package directory as the working directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	td, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	return td
}

// loadFindings loads one testdata package in module mode (the testdata go.mod
// has a local replace onto the real framework, so the fixtures import the real
// embed.Surface / app.Screen / component types) and runs findFindings over it.
// This exercises the exact resolution path the CLI and `gofastr build` gate
// use, against real Go source. It deliberately bypasses analysistest's `// want`
// checker so the mutation suite can assert before/after counts without fighting
// static expectations.
func loadFindings(t *testing.T, pkgName string) []Finding {
	t.Helper()
	findings, _ := loadAll(t, pkgName)
	return findings
}

// loadAll is loadFindings plus the walk's give-up notes.
func loadAll(t *testing.T, pkgName string) ([]Finding, []Unresolved) {
	t.Helper()
	dir := testdataDir(t)
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedDeps,
		Fset:  fset,
		Dir:   dir,
		Tests: false,
		// testdata/go.mod is its own module with a local replace; module mode +
		// GOPROXY=off resolves everything from the working tree and cache.
		Env: append(os.Environ(), "GO111MODULE=on", "GOPROXY=off", "GOWORK=off"),
	}
	pkgs, err := packages.Load(cfg, "./src/"+pkgName)
	if err != nil {
		t.Fatalf("loading %s: %v", pkgName, err)
	}
	var out []Finding
	var notes []Unresolved
	loaded := false
	for _, p := range pkgs {
		if p.Types == nil || p.TypesInfo == nil {
			for _, e := range p.Errors {
				t.Logf("package %s error: %v", p.ID, e)
			}
			continue
		}
		loaded = true
		f, u := analyze(p.Types, p.TypesInfo, p.Syntax)
		out = append(out, f...)
		notes = append(notes, u...)
	}
	if !loaded {
		t.Fatalf("package %s did not type-check (see logged errors above)", pkgName)
	}
	return out, notes
}

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
// that is NOT on any embeddable surface must not trip the gate — the gate
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
// Each proves its static test is actually exercising the trigger: the source
// is rewritten on disk, the mutation is verified to have applied (occurrence
// count before/after), and the finding count must flip. A mutation that
// silently did not apply looks exactly like a test that cannot fail.

// replacement is one (from→to) edit applied to a fixture file.
type replacement struct{ from, to string }

// mutateFixture rewrites relPath on disk by applying each replacement exactly
// once, in order, and asserts each one actually changed the content. An edit
// whose `from` is absent is a silent no-op — exactly the failure mode that
// makes a mutated test indistinguishable from an inert one, which has bitten
// this repo before. The before/after occurrence count of each `to` is logged so
// the test output proves the mutation landed. It returns the original bytes.
func mutateFixture(t *testing.T, relPath string, reps ...replacement) []byte {
	t.Helper()
	orig, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	cur := string(orig)
	for _, r := range reps {
		if !strings.Contains(cur, r.from) {
			t.Fatalf("mutation did not apply: from-pattern not found in %s\n%q", relPath, r.from)
		}
		before := strings.Count(cur, r.to)
		cur = strings.Replace(cur, r.from, r.to, 1)
		after := strings.Count(cur, r.to)
		t.Logf("applied in %s: %q removed; %q occurrences %d→%d", relPath, r.from, r.to, before, after)
	}
	if err := os.WriteFile(relPath, []byte(cur), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
	return orig
}

func restore(t *testing.T, relPath string, orig []byte) {
	t.Helper()
	if err := os.WriteFile(relPath, orig, 0o644); err != nil {
		t.Fatalf("restore %s: %v", relPath, err)
	}
}

// TestMutationCleanBecomesFlagged: turning the clean surface's setState action
// into a server action must produce exactly one finding — proving the clean
// case passed because it had no server action, not because detection is inert.
func TestMutationCleanBecomesFlagged(t *testing.T) {
	const rel = "testdata/src/clean/clean.go"
	orig := mutateFixture(t, rel, replacement{
		from: "G.setState('count', 1)",
		to:   `G.serverAction(\"save\")`,
	})
	defer restore(t, rel, orig)
	if got := loadFindings(t, "clean"); len(got) != 1 {
		t.Fatalf("after mutation, expected 1 finding on clean, got %d: %+v", len(got), got)
	}
}

// TestMutationBadBecomesClean: removing the bad surface's server action must
// drop the finding count to zero — proving the finding was due to the
// G.serverAction token, not a side effect.
func TestMutationBadBecomesClean(t *testing.T) {
	const rel = "testdata/src/bad/bad.go"
	orig := mutateFixture(t, rel, replacement{
		from: `G.serverAction("save")`,
		to:   "G.setState('saved', true)",
	})
	defer restore(t, rel, orig)
	if got := loadFindings(t, "bad"); len(got) != 0 {
		t.Fatalf("after mutation, expected 0 findings on bad, got %d: %+v", len(got), got)
	}
}

// TestMutationNonEmbeddableBecomesFlagged: putting the notembeddable component
// (which already registers a server action) onto an embeddable surface must
// produce a finding — proving reachability from an embed.Surface, not the
// action itself, is what the gate keys on. Two edits land the surface: the
// fembed import and the Surface declaration that replaces RegisterScreen.
func TestMutationNonEmbeddableBecomesFlagged(t *testing.T) {
	const rel = "testdata/src/notembeddable/notembeddable.go"
	orig := mutateFixture(t, rel,
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
	defer restore(t, rel, orig)
	if got := loadFindings(t, "notembeddable"); len(got) != 1 {
		t.Fatalf("after mutation, expected 1 finding on notembeddable, got %d: %+v", len(got), got)
	}
}

func TestMutationResolvedShapesBecomeClean(t *testing.T) {
	fixtures := []struct {
		pkgName string
		from    string
	}{
		{pkgName: "interfacecomponent", from: `G.serverAction("save")`},
		{pkgName: "chained", from: `G.serverAction("save")`},
		{pkgName: "inline", from: `G.serverAction("save")`},
		{pkgName: "childaction", from: `G.serverAction("save")`},
		{pkgName: "whitespace", from: `G.serverAction ("save")`},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.pkgName, func(t *testing.T) {
			rel := filepath.Join("testdata", "src", fixture.pkgName, fixture.pkgName+".go")
			orig := mutateFixture(t, rel, replacement{
				from: fixture.from,
				to:   `G.setState("saved", true)`,
			})
			defer restore(t, rel, orig)
			if got := loadFindings(t, fixture.pkgName); len(got) != 0 {
				t.Fatalf("after mutation, expected 0 findings on %s, got %d: %+v",
					fixture.pkgName, len(got), got)
			}
		})
	}
}
