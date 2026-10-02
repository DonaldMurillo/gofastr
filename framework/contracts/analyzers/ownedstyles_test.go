package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The owned-style rules at verify time. The CSS-only checks are
// core-ui/ownstyle's (the ones gen styles runs); these tests pin that
// verify reports them at the CSS line:column with the catalog's
// severity, once per finding, and that the program-wide and Go-side
// rules (1815–1818) read what they claim to.

// ownedPair returns a <name>.style.css and its fresh generated sibling
// in package pkg, keyed for fixture(): dir/name.style.css and
// dir/name_style.gen.go.
func ownedPair(t *testing.T, dir, pkg, name, css string) map[string]string {
	t.Helper()
	kind := ownstyle.KindScoped
	if name == "app" {
		kind = ownstyle.KindApp
	}
	sheet, diags := ownstyle.Parse(css)
	if len(diags) > 0 {
		t.Fatalf("fixture CSS does not parse: %v", diags)
	}
	model, _ := ownstyle.Model(sheet)
	gen, err := ownstyle.GenerateFile(name, kind, css, model, pkg, false)
	if err != nil {
		t.Fatalf("generate %s: %v", name, err)
	}
	return map[string]string{
		dir + "/" + ownstyle.StyleFileName(name):     css,
		dir + "/" + ownstyle.GeneratedFileName(name): gen,
	}
}

// merge folds fixture maps together.
func merge(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// cssOnly is one owned style with its generated pair, for the rules
// that read only the CSS.
func cssOnly(t *testing.T, name, css string) []contracts.Diagnostic {
	t.Helper()
	return fixture(t, ownedPair(t, "board", "board", name, css))
}

// fireAndQuiet pins one CSS rule: bad fires exactly once, at want
// line:col; good does not fire.
func fireAndQuiet(t *testing.T, rule, name, bad string, line, col int, good string) contracts.Diagnostic {
	t.Helper()
	found := countRule(t, cssOnly(t, name, bad), rule)
	if len(found) != 1 {
		t.Fatalf("%s: want exactly one finding on %q, got %v", rule, bad, found)
	}
	d := found[0]
	if d.File != "board/"+ownstyle.StyleFileName(name) || d.Line != line || d.Column != col {
		t.Errorf("%s at %s:%d:%d, want board/%s:%d:%d", rule, d.File, d.Line, d.Column, ownstyle.StyleFileName(name), line, col)
	}
	assertNot(t, cssOnly(t, name, good), rule, "the documented fix")
	return d
}

func TestOwnedUnknownTokenOnce(t *testing.T) {
	fireAndQuiet(t, contracts.RuleUnknownThemeToken, "board",
		".a {\n  color: var(--colr-primary);\n}\n", 2, 14,
		".a {\n  color: var(--color-primary);\n}\n")
}

func TestOwnedHardcodedTokenValue(t *testing.T) {
	xs := style.ThemeToTokens(style.DefaultTheme())["text-xs"]
	if xs == "" {
		t.Fatal("theme has no text-xs; the fixture needs a token value")
	}
	fireAndQuiet(t, contracts.RuleHardcodedTokenValue, "board",
		".a { font-size: "+xs+"; }\n", 1, 17,
		".a { font-size: var(--text-xs); }\n")
}

func TestOwnedFallbackDrift(t *testing.T) {
	md := style.ThemeToTokens(style.DefaultTheme())["spacing-md"]
	fireAndQuiet(t, contracts.RuleFallbackDrift, "board",
		".a { padding: var(--spacing-md, 13px); }\n", 1, 15,
		".a { padding: var(--spacing-md, "+md+"); }\n")
}

func TestOwnedKitClassSelector(t *testing.T) {
	fireAndQuiet(t, contracts.RuleKitClassSelector, "board",
		".col .fui-card-header { gap: 0; }\n", 1, 6,
		".col .title { gap: 0; }\n")
}

func TestOwnedImportant(t *testing.T) {
	fireAndQuiet(t, contracts.RuleImportant, "board",
		".a { order: 1 !important; }\n", 1, 15,
		".a { order: 1; }\n")
}

func TestOwnedRawMediaWidth(t *testing.T) {
	fireAndQuiet(t, contracts.RuleRawMediaWidth, "board",
		"@media (min-width: 700px) { .a { order: 1; } }\n", 1, 20,
		"@media (--above-md) { .a { order: 1; } }\n")
}

func TestOwnedAnimationIsWarn(t *testing.T) {
	d := fireAndQuiet(t, contracts.RuleAnimationNoReduced, "board",
		"@keyframes pulse { to { opacity: 0; } }\n.a { animation: pulse 1s; }\n", 2, 6,
		"@keyframes pulse { to { opacity: 0; } }\n.a { animation: pulse 1s; }\n@media (--reduced-motion) { .a { animation: none; } }\n")
	if d.Severity != contracts.SeverityWarn {
		t.Errorf("severity = %s, want warn (the catalog's)", d.Severity)
	}
}

func TestOwnedAppSheetSelector(t *testing.T) {
	fireAndQuiet(t, contracts.RuleAppSheetSelector, "app",
		"h2 { order: 1; }\n", 1, 1,
		".title { order: 1; }\n")
}

func TestOwnedTokenCustomProperty(t *testing.T) {
	fireAndQuiet(t, contracts.RuleTokenCustomProperty, "board",
		":scope { --color-primary: red; }\n", 1, 10,
		":scope { --board-accent: var(--color-primary); }\n")
}

// testdata trees hold parser fixtures; the owned-style checks skip
// them, like 1814.
func TestOwnedChecksSkipTestdata(t *testing.T) {
	ds := fixture(t, map[string]string{
		"testdata/board.style.css": ".a { order: 1 !important; }\n",
	})
	assertNot(t, ds, contracts.RuleImportant, "testdata is not a shipped package")
	assertNot(t, ds, contracts.RuleUpstreamCandidate, "testdata is not a shipped package")
}

func TestUpstreamCandidateListsEverySheet(t *testing.T) {
	ds := fixture(t, merge(
		ownedPair(t, "board", "board", "board", ":scope.fresh { order: 1; }\n.column { order: 1; }\n.column.over-limit { order: 2; }\n.card.priority--urgent { order: 3; }\n"),
		ownedPair(t, "site", "site", "app", ".lede { order: 1; }\n"),
	))
	found := countRule(t, ds, contracts.RuleUpstreamCandidate)
	if len(found) != 2 {
		t.Fatalf("want one listing per owned sheet, got %v", found)
	}
	for _, d := range found {
		if d.Severity != contracts.SeverityInfo {
			t.Errorf("%s: severity %s, want info", d.File, d.Severity)
		}
		switch d.File {
		case "board/board.style.css":
			// fresh, column, over-limit, card, priority--urgent.
			if !strings.Contains(d.Message, `"board"`) || !strings.Contains(d.Message, "5 classes") {
				t.Errorf("board listing = %q, want the owner name and 5 classes", d.Message)
			}
		case "site/app.style.css":
			if !strings.Contains(d.Message, `"app"`) || !strings.Contains(d.Message, "1 class;") {
				t.Errorf("app listing = %q, want the owner name and 1 class", d.Message)
			}
		default:
			t.Errorf("unexpected listing for %s", d.File)
		}
	}
	assertNot(t, fixture(t, map[string]string{"main.go": "package main\n"}), contracts.RuleUpstreamCandidate,
		"no owned sheet, nothing to list")
}

func TestDuplicateStyleNameAcrossDirs(t *testing.T) {
	ds := fixture(t, merge(
		ownedPair(t, "board", "board", "card", ".a { order: 1; }\n"),
		ownedPair(t, "review", "review", "card", ".b { order: 1; }\n"),
	))
	found := countRule(t, ds, contracts.RuleDuplicateStyleName)
	if len(found) != 2 {
		t.Fatalf("want both files of the duplicate reported, got %v", found)
	}
	for _, d := range found {
		other := "review/card.style.css"
		if d.File == other {
			other = "board/card.style.css"
		}
		if !strings.Contains(d.Message, other) {
			t.Errorf("%s: message %q does not name %s", d.File, d.Message, other)
		}
	}
	ds = fixture(t, merge(
		ownedPair(t, "board", "board", "board-card", ".a { order: 1; }\n"),
		ownedPair(t, "review", "review", "review-card", ".b { order: 1; }\n"),
	))
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "distinct names")
}

// boardCSS is the 1817 sheet: .column is handed to a kit root; .lane
// is not.
const boardCSS = ":scope { display: grid; }\n" +
	".column {\n  grid-column: span 2;\n  padding: var(--spacing-md);\n}\n" +
	".column.over-limit { color: var(--color-danger); }\n" +
	".lane { padding: var(--spacing-md); }\n"

const kitViewPrefix = "package view\n\nimport (\n\t\"example.com/app/board\"\n\t\"github.com/DonaldMurillo/gofastr/framework/ui\"\n)\n\n"

func TestKitRootStyleFiresOnClassField(t *testing.T) {
	ds := fixture(t, merge(ownedPair(t, "board", "board", "board", boardCSS), map[string]string{
		"view/view.go": kitViewPrefix + "func v() any { return ui.Card(ui.CardConfig{Class: board.Style.Column()}) }\n",
	}))
	found := countRule(t, ds, contracts.RuleKitRootStyle)
	// padding on .column (line 4) and color on .column.over-limit
	// (line 6); grid-column is placement, and .lane never reaches a kit
	// root.
	if len(found) != 2 {
		t.Fatalf("want padding and color on .column, got %v", found)
	}
	if d := found[0]; d.File != "board/board.style.css" || d.Line != 4 || d.Column != 3 ||
		!strings.Contains(d.Message, "ui.Card's Class at view/view.go:8") {
		t.Errorf("first finding = %s:%d:%d %q", d.File, d.Line, d.Column, d.Message)
	}
	if found[1].Line != 6 {
		t.Errorf("second finding at line %d, want 6 (.column.over-limit)", found[1].Line)
	}
}

func TestKitRootStyleFiresOnWithCall(t *testing.T) {
	ds := fixture(t, merge(ownedPair(t, "board", "board", "board", boardCSS), map[string]string{
		"view/view.go": kitViewPrefix + "func v() any {\n\treturn ui.Card(ui.CardConfig{Class: board.Style.ColumnWith(board.ColumnVariants{OverLimit: true})})\n}\n",
	}))
	if len(countRule(t, ds, contracts.RuleKitRootStyle)) != 2 {
		t.Fatalf("ColumnWith(...) puts .column on the kit root; want its two findings, got %v",
			countRule(t, ds, contracts.RuleKitRootStyle))
	}
}

func TestKitRootStyleFiresOnScopedKitRoot(t *testing.T) {
	// A component whose own root is a kit root: its :scope rules are
	// placement-only. Same package, bare Style.
	ds := fixture(t, merge(ownedPair(t, "board", "board", "board", boardCSS), map[string]string{
		"board/card.go": "package board\n\nimport \"github.com/DonaldMurillo/gofastr/framework/ui\"\n\n" +
			"func card() any { return Style.Scope(ui.Card(ui.CardConfig{})) }\n",
	}))
	assertNot(t, ds, contracts.RuleKitRootStyle, ":scope sets only display, a placement property")

	css := ":scope { display: grid; background: var(--color-surface); }\n"
	ds = fixture(t, merge(ownedPair(t, "board", "board", "board", css), map[string]string{
		"board/card.go": "package board\n\nimport \"github.com/DonaldMurillo/gofastr/framework/ui\"\n\n" +
			"func card() any { return Style.Scope(ui.Card(ui.CardConfig{})) }\n",
	}))
	found := countRule(t, ds, contracts.RuleKitRootStyle)
	if len(found) != 1 || found[0].Line != 1 || found[0].Column != 25 {
		t.Fatalf("want background at 1:25, got %v", found)
	}
}

func TestKitRootStyleQuietOffKitRoots(t *testing.T) {
	ds := fixture(t, merge(ownedPair(t, "board", "board", "board", boardCSS), map[string]string{
		// .column on the handle's own markup, and a Scope on plain
		// markup: neither is a kit root.
		"view/view.go": "package view\n\nimport (\n\t\"example.com/app/board\"\n\t\"github.com/DonaldMurillo/gofastr/core-ui/html\"\n)\n\n" +
			"func v() any { return board.Style.Scope(html.Div(html.DivConfig{Class: board.Style.Column()})) }\n",
		// A kit root given only placement classes.
		"view/kit.go": "package view\n\nimport \"github.com/DonaldMurillo/gofastr/framework/ui\"\n\n" +
			"func k() any { return ui.Card(ui.CardConfig{Class: \"plain\"}) }\n",
	}))
	assertNot(t, ds, contracts.RuleKitRootStyle, "no class reaches a framework/ui root")
}

// screenPair is a board package whose screen attaches its own style.
func screenPair(t *testing.T, attach string) map[string]string {
	return merge(ownedPair(t, "board", "board", "board", ".column { order: 1; }\n"), map[string]string{
		"board/screen.go": "package board\n\nimport uiapp \"github.com/DonaldMurillo/gofastr/core-ui/app\"\n\n" + attach +
			"\n\nfunc column() string { return Style.Column() }\n",
	})
}

const widgetsUse = "package widgets\n\nimport \"example.com/app/board\"\n\nfunc lane() string {\n\treturn board.Style.Column()\n}\n"

func TestOwnedHandleLeakFromScreen(t *testing.T) {
	ds := fixture(t, merge(screenPair(t, `var S = uiapp.NewScreen("/board", nil).WithStyle(Style)`),
		map[string]string{"widgets/lane.go": widgetsUse}))
	found := countRule(t, ds, contracts.RuleOwnedHandleLeak)
	if len(found) != 1 {
		t.Fatalf("want the widgets use only (the board package's own use is its owner's), got %v", found)
	}
	d := found[0]
	if d.File != "widgets/lane.go" || d.Line != 6 || d.Column != 9 || !strings.Contains(d.Message, "Screen.WithStyle in board") {
		t.Errorf("finding = %s:%d:%d %q", d.File, d.Line, d.Column, d.Message)
	}
}

func TestOwnedHandleLeakFromLayout(t *testing.T) {
	ds := fixture(t, merge(screenPair(t, `var L = uiapp.LayoutSpec{Name: "board", Style: Style}`),
		map[string]string{"widgets/lane.go": widgetsUse}))
	if len(countRule(t, ds, contracts.RuleOwnedHandleLeak)) != 1 {
		t.Fatalf("a layout-owned handle used in widgets; got %v", countRule(t, ds, contracts.RuleOwnedHandleLeak))
	}
}

func TestOwnedHandleLeakQuietForComponent(t *testing.T) {
	// Not attached to a layout or screen: a component's style is used
	// wherever the component is, and 1818 has nothing to say.
	ds := fixture(t, merge(
		screenPair(t, `var _ = uiapp.LayoutSpec{Name: "board"}`),
		map[string]string{"widgets/lane.go": widgetsUse},
	))
	assertNot(t, ds, contracts.RuleOwnedHandleLeak, "the handle is not screen- or layout-owned")
}

// The documented pattern attaches a screen's style where the app
// registers it (main: .WithStyle(board.Style)) while the screen's own
// package renders with the classes. The handle's home package is always
// its owner's; only a third package is a leak.
func TestOwnedHandleLeakQuietInHomePkg(t *testing.T) {
	ds := fixture(t, merge(screenPair(t, `var _ = uiapp.NewScreen`), map[string]string{
		"main.go":         "package main\n\nimport (\n\tuiapp \"github.com/DonaldMurillo/gofastr/core-ui/app\"\n\t\"example.com/app/board\"\n)\n\nvar S = uiapp.NewScreen(\"/board\", nil).WithStyle(board.Style)\n",
		"widgets/lane.go": widgetsUse,
	}))
	found := countRule(t, ds, contracts.RuleOwnedHandleLeak)
	if len(found) != 1 || found[0].File != "widgets/lane.go" {
		t.Fatalf("want only the widgets use; the board package's own Style.Column() is its owner's, got %v", found)
	}
}
