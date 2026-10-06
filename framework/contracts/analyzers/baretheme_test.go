package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1823: a kit sheet that writes a line width, a pill radius, a
// motion duration or a stacking layer as a bare literal. GOFASTR1807
// judges a value whole, so `border: 1px solid …` and
// `transition: color 150ms ease` passed it, and a theme that set
// --stroke-thin to 3px or --radii-full to 0 never reached those
// declarations.
func TestBareThemeLiteralsAreReported(t *testing.T) {
	for _, tc := range []struct {
		css, want string
	}{
		{".a { border: 1px solid var(--color-border); }", "--stroke-thin"},
		{".a { border-block-end: 2px dashed red; }", "--stroke-"},
		{".a { border-inline-start-width: 3px; }", "--stroke-"},
		{".a { outline: 2px solid var(--color-text-subtle); }", "--stroke-focus"},
		{".a { outline-offset: -2px; }", "--stroke-focus-offset"},
		{".a { box-shadow: inset 0 0 0 1px var(--color-border); }", "--stroke-"},
		{".a { border-radius: 999px; }", "--radii-full"},
		{".a { border-top-left-radius: 2px; }", "--radii-sm"},
		{".a { transition: color 150ms ease, background .2s; }", "--duration-"},
		{".a { animation: spin 120ms ease-out; }", "--duration-"},
		{".a { z-index: 9999; }", "--z-"},
		{".a { border-width: calc(1px + 1px); }", "--stroke-"},
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\nvar css = `"+tc.css+"`\n")
		found := countRule(t, ds, contracts.RuleBareThemeLiteral)
		if len(found) != 1 {
			t.Errorf("%s: want 1 GOFASTR1823, got %d: %v", tc.css, len(found), found)
			continue
		}
		if !strings.Contains(found[0].Message, tc.want) {
			t.Errorf("%s: message does not name %s: %q", tc.css, tc.want, found[0].Message)
		}
	}
}

func TestBareThemeLiteralInSetPairIsReported(t *testing.T) {
	ds := designFixture(t, "core-ui/widget/page.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".card\").Set(\"border\", \"1px solid {colors.border}\").End()\n}\n")
	if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 1 {
		t.Fatalf("want 1 GOFASTR1823 on the Set pair, got %d: %v", len(found), found)
	}
}

// What stays legal: a token with its fallback, a zero width, a local
// stacking order, a loop period, a structural length, and CSS outside
// the design-system trees (GOFASTR1801's finding there).
func TestBareThemeLiteralQuietCases(t *testing.T) {
	for _, css := range []string{
		".a { border: var(--stroke-thin, 1px) solid var(--color-border); }",
		".a { outline-offset: calc(-1 * var(--stroke-focus-offset, 2px)); }",
		".a { border: 0; outline: 0; border-width: 0; }",
		".a { border-radius: var(--radii-full, 9999px); }",
		".a { border-radius: 50%; }",
		".a { border-radius: var(--radii-md, 8px) var(--radii-md, 8px) 0 0; }",
		".a { border-radius: calc(var(--radii-sm, 6px) / 3); }",
		".a { border-radius: calc(var(--radii-md, 8px) - 2px); }",
		".a { transition: color var(--duration-fast, 150ms) ease; }",
		".a { animation: spin 1s linear infinite; }",
		".a { animation-delay: 120ms; }",
		".a { z-index: 2; }",
		".a { z-index: var(--z-modal, 300); }",
		".a { inline-size: 1px; block-size: 1px; margin: -1px; }",
		".a { box-shadow: 0 1px 2px rgba(0,0,0,.05); }",
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\nvar css = `"+css+"`\n")
		if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 0 {
			t.Errorf("%s: unexpected %v", css, found)
		}
	}
	ds := fixture(t, map[string]string{"internal/app/x.go": "package app\n\nvar css = `.a { border: 1px solid red; }`\n"})
	if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 0 {
		t.Errorf("fires outside the design-system trees: %v", found)
	}
}
