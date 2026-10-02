package scan

import (
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
