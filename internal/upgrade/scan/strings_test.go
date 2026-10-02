package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// classesNote builds a note matching the given class names.
func classesNote(names ...string) *upgrade.Note {
	return &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{Classes: names}}}
}

func attrsNote(names ...string) *upgrade.Note {
	return &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{Attrs: names}}}
}

// mustRunWithSinks runs the engine with the stub kit's marker sinks.
func mustRunWithSinks(t *testing.T, root string, n *upgrade.Note) *Result {
	t.Helper()
	testEnv(t)
	res, err := Run(root, []*upgrade.Note{n}, testSinks())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestClassesLiteral(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"class": "card ui-button"}
}
`
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, `"card ui-button"`, "main.go", "class ui-button"))
}

func TestClassesConcatFoldedOnce(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"class": "card " +
		"ui-button"}
}
`
	// The concatenation is one constant value: one hit at the fold's start,
	// not two at the operands.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, `"card " +`, "main.go", "class ui-button"))
}

func TestClassesBEMForms(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"a": "ui-button--lg ui-button__icon ui-card"}
}
`
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	// Both BEM forms match the one value; ui-card is another component.
	wantHits(t, res, n, hitAt(src, `"ui-button--lg`, "main.go", "class ui-button"))
}

func TestClassesMarkupClassAttrOnly(t *testing.T) {
	src := "package main\n" +
		"\n" +
		"func main() {\n" +
		"\t_ = `<a class=\"ui-button card\" aria-label=\"ui-button\">x</a>`\n" +
		"}\n"
	// Inside markup only tokens within class="..." count: one hit for the
	// class attribute, and the aria-label occurrence stays silent.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "`<a class=", "main.go", "class ui-button"))
}

func TestClassesMarkerSinkCallSilent(t *testing.T) {
	src := `package main

import "example.com/kit/registry"

func main() {
	registry.RegisterStyle("ui-button", "color:red")
}
`
	// RegisterStyle's first argument takes a component name, not a class.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestClassesMarkerSinkFieldSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.SidebarConfig{DrawerName: "ui-button"}
}
`
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestClassesAttrKeySinkSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"data-fui-comp": "ui-button"}
}
`
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestClassesConstAllSinkUsesSilent(t *testing.T) {
	src := `package main

import "example.com/kit/registry"

const sheetName = "ui-button"

func main() {
	registry.RegisterStyle(sheetName, "color:red")
}
`
	// The constant's only use is the marker-sink call.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestClassesConstSinkInOtherFile(t *testing.T) {
	names := "package main\n\nconst sheetName = \"ui-button\"\n"
	use := `package main

import "example.com/kit/registry"

func main() {
	registry.RegisterStyle(sheetName, "color:red")
}
`
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"names.go": names, "main.go": use}), n)
	wantHits(t, res, n)
}

func TestClassesInCSSStringHit(t *testing.T) {
	src := "package main\n\nvar css = `.ui-button:hover { color: red }\n.ui-card--flat{}`\n\nfunc main() { _ = css }\n"
	n := classesNote("ui-button", "ui-card")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, "main.go:3:11 class ui-button")
}

func TestClassesCSSStringLongerSilent(t *testing.T) {
	src := "package main\n\nvar css = `.ui-button-group { color: red } a.ui-buttons{}`\n\nfunc main() { _ = css }\n"
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestTestingMessageSilent(t *testing.T) {
	src := `package main

import "testing"

func TestPage(t *testing.T) {
	t.Fatal("no ui-button on the page")
	t.Errorf("want %s", "data-fui-signal" + " set")
	t.Run("ui-button renders", func(t *testing.T) {})
	var tb testing.TB = t
	tb.Log("ui-button")
	if "ui-button" == "" {
		t.Skip()
	}
}
`
	n := &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{Classes: []string{"ui-button"}, Attrs: []string{"data-fui-signal"}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "page_test.go": src}), n)
	// Only the comparison operand is markup a test asserts on; the
	// failure messages, the subtest name and the TB log are prose.
	wantHits(t, res, n, hitAt(src, `"ui-button" ==`, "page_test.go", "class ui-button"))
}

func TestClassesVarUsedAsClassHit(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	cls := "ui-button"
	_ = ui.Attrs{"class": cls}
}
`
	// The variable's use is a class slot, not a marker sink, so its
	// initialiser is a hit.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, `"ui-button"`, "main.go", "class ui-button"))
}

func TestClassesLongerTokenSilent(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"class": "ui-button-radius"}
}
`
	// A longer token is a different class, not a BEM form.
	n := classesNote("ui-button")
	res := mustRunWithSinks(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n)
}

func TestAttrsMapKey(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{"data-fui-signal": ""}
}
`
	n := attrsNote("data-fui-signal")
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, `"data-fui-signal"`, "main.go", "attr data-fui-signal"))
}

func TestAttrsPrefixForm(t *testing.T) {
	src := `package main

import "example.com/kit/ui"

func main() {
	_ = ui.Attrs{
		"data-fui-toggle-open": "",
		"data-fui-toggle-close": "",
		"data-fui-other":       "",
	}
}
`
	n := attrsNote("data-fui-toggle-")
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"data-fui-toggle-open"`, "main.go", "attr data-fui-toggle-"),
		hitAt(src, `"data-fui-toggle-close"`, "main.go", "attr data-fui-toggle-"))
}

func TestAttrsInsideMarkup(t *testing.T) {
	src := "package main\n" +
		"\n" +
		"func main() {\n" +
		"\t_ = `<input data-fui-signal=\"\" data-x-fui-signal=\"\">`\n" +
		"}\n"
	// The attribute inside markup matches; the one whose name merely
	// contains the string does not (no left boundary).
	n := attrsNote("data-fui-signal")
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, "`<input", "main.go", "attr data-fui-signal"))
}

func TestAttrsProseMentionIsHit(t *testing.T) {
	src := `package main

func main() {
	_ = "see data-fui-signal docs"
}
`
	// The boundary rule (whitespace before and after) accepts a prose
	// mention; that is what the rule gives.
	n := attrsNote("data-fui-signal")
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n, hitAt(src, `"see data-fui-signal`, "main.go", "attr data-fui-signal"))
}

func TestPropertiesVarRead(t *testing.T) {
	src := `package main

func main() {
	_ = "color: var(--color-muted)"
	_ = "color: var(--color-muted-2)"
}
`
	n := &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{Properties: []string{"--color-muted"}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	// --color-muted-2 is a different property: the token boundary holds.
	wantHits(t, res, n, hitAt(src, `"color: var(--color-muted)"`, "main.go", "property --color-muted"))
}

func TestMatchRegex(t *testing.T) {
	src := `package main

func main() {
	_ = "X-Gofastr-Infinite-Cursor: 1"
	_ = "X-Other: 2"
}
`
	n := &upgrade.Note{Find: upgrade.Find{Strings: upgrade.StringMatch{
		Match: regexp.MustCompile(`^X-Gofastr-Infinite-Cursor:`),
	}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": src}), n)
	wantHits(t, res, n,
		hitAt(src, `"X-Gofastr-Infinite-Cursor`, "main.go", "match ^X-Gofastr-Infinite-Cursor:"))
}
