package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// cssWorkspace builds an app whose only Go file compiles, plus stylesheets.
func cssWorkspace(t *testing.T, files map[string]string) string {
	files["main.go"] = "package main\n\nfunc main() {}\n"
	return newWorkspace(t, defaultKit, files)
}

func cssNote(classes, properties []string) *upgrade.Note {
	return &upgrade.Note{Find: upgrade.Find{CSS: upgrade.CSSMatch{Classes: classes, Properties: properties}}}
}

func TestCSSClassSelector(t *testing.T) {
	css := `.ui-button {
	color: red;
}
.ui-card {
	color: blue;
}
`
	n := cssNote([]string{"ui-button"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n, hitAt(css, "ui-button {", "static/app.style.css", "css .ui-button"))
}

func TestCSSNestedRule(t *testing.T) {
	css := `.board {
	.ui-button {
		color: red;
	}
}
`
	n := cssNote([]string{"ui-button"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n, hitAt(css, "ui-button {", "static/app.style.css", "css .ui-button"))
}

func TestCSSInsideMedia(t *testing.T) {
	css := `@media (min-width: 600px) {
	.ui-button {
		color: red;
	}
}
`
	n := cssNote([]string{"ui-button"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n, hitAt(css, "ui-button {", "static/app.style.css", "css .ui-button"))
}

func TestCSSBEMForm(t *testing.T) {
	css := `.ui-button--lg {
	color: red;
}
.ui-button__icon {
	color: blue;
}
.ui-button-radius {
	color: green;
}
`
	n := cssNote([]string{"ui-button"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n,
		hitAt(css, "ui-button--lg", "static/app.style.css", "css .ui-button"),
		hitAt(css, "ui-button__icon", "static/app.style.css", "css .ui-button"))
}

func TestCSSDeclValueSilent(t *testing.T) {
	css := `.card {
	content: ".ui-button";
	grid-template-areas: . ui-button b;
}
`
	// A class name inside a declaration value (a string, or the grid area
	// "." cell spelling) is not a selector.
	n := cssNote([]string{"ui-button"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n)
}

func TestCSSPropertyDeclAndVar(t *testing.T) {
	css := `.card {
	--color-muted: #888;
	color: var(--color-muted);
	border: 1px solid var(--color-muted-2);
}
`
	n := cssNote(nil, []string{"--color-muted"})
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	// The declaration and the var() read both hit, at the ident token.
	wantHits(t, res, n,
		hitAt(css, "--color-muted:", "static/app.style.css", "css --color-muted"),
		hitAt(css, "--color-muted)", "static/app.style.css", "css --color-muted"))
}

func TestCSSEscapedClassSelector(t *testing.T) {
	css := ".ui\\2d button {\n\tcolor: red;\n}\n.ui\\-card {\n\tcolor: blue;\n}\n.ui\\00002d button {\n\tcolor: green;\n}\n"
	// A hex escape (with the one whitespace it swallows) or an escaped
	// character names the same class: .ui\2d button is .ui-button.
	n := cssNote([]string{"ui-button", "ui-card"}, nil)
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n,
		hitAt(css, `ui\2d`, "static/app.style.css", "css .ui-button"),
		hitAt(css, `ui\-card`, "static/app.style.css", "css .ui-card"),
		hitAt(css, `ui\0000`, "static/app.style.css", "css .ui-button"))
}

func TestCSSEscapedProperty(t *testing.T) {
	css := ".x {\n\t\\-\\-ui-gap: 1px;\n\tmargin: var(--ui\\2d gap);\n}\n"
	n := cssNote(nil, []string{"--ui-gap"})
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	if got := len(res.Hits[n]); got != 2 {
		t.Fatalf("hits = %v, want the escaped declaration and the escaped var() argument", hitStrs(res.Hits[n]))
	}
}

func TestCSSDeclarationValue(t *testing.T) {
	css := `.ring:focus-visible {
	outline: 2px solid red;
	outline-offset: 2px;
}
.lift {
	outline-offset:2px !important;
	outline-offset: -2px;
	outline-offset: var(--stroke-focus-offset, 2px);
	--outline-offset: 2px;
	content: "outline-offset: 2px";
}
outline-offset:hover {
	color: red;
}
`
	// The value is read whole, !important dropped: a custom property of
	// the same name, a var() fallback, a string and a selector that
	// happens to start with the name are all silent.
	n := &upgrade.Note{Find: upgrade.Find{CSS: upgrade.CSSMatch{Declarations: []upgrade.CSSDeclaration{
		{Properties: []string{"outline-offset"}, Value: regexp.MustCompile(`^[12]px$`)},
	}}}}
	res := mustRun(t, cssWorkspace(t, map[string]string{"site/site.style.css": css}), n)
	wantHits(t, res, n,
		hitAt(css, "outline-offset: 2px;", "site/site.style.css", "css outline-offset: 2px"),
		hitAt(css, "outline-offset:2px", "site/site.style.css", "css outline-offset: 2px"))
}

func TestCSSDeclarationStartsAtBlock(t *testing.T) {
	css := `.x {
	grid-area: a b: 1px;
	b: 1px;
	.y {
		b:hover {
			color: red;
		}
	}
}
`
	// Only an ident right after "{" or ";" names a property, and a run
	// that reaches "{" was a nested rule's selector.
	n := &upgrade.Note{Find: upgrade.Find{CSS: upgrade.CSSMatch{Declarations: []upgrade.CSSDeclaration{
		{Properties: []string{"b"}, Value: regexp.MustCompile(`.`)},
	}}}}
	res := mustRun(t, cssWorkspace(t, map[string]string{"site/site.style.css": css}), n)
	wantHits(t, res, n, hitAt(css, "b: 1px;\n\t.y", "site/site.style.css", "css b: 1px"))
}
