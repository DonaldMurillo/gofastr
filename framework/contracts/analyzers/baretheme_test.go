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
		{".a { transition: color var(--duration-fast, 150ms) ease; }", "--easing-"},
		{".a { transition-timing-function: ease-in-out; }", "--easing-"},
		{".a { animation: pop var(--duration-fast, 150ms) cubic-bezier(0.2, 0, 0, 1); }", "--easing-"},
		{".a { transition-delay: 80ms; }", "--duration-"},
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
		".a { transition: color var(--duration-fast, 150ms) var(--easing-ease-out, ease); }",
		".a { animation: spin 1s linear infinite; }",
		".a { animation: blink 1s steps(2, start) infinite; }",
		".a { animation: ease-pulse 1s linear infinite; }",
		".a { animation-delay: 120ms; }",
		".a { transition-delay: 0s; transition-delay: 600ms; }",
		".a { z-index: 2; }",
		".a { z-index: var(--z-modal, 300); }",
		".a { inline-size: 1px; block-size: 1px; margin: -1px; }",
		".a { box-shadow: 0 1px 0 var(--color-border); }",
		".a { box-shadow: var(--shadow-md); }",
		".a { padding: var(--spacing-md, 8px) calc(var(--spacing-sm, 4px) * 1.5); }",
		".a { gap: 0; padding: 1px; margin-block: 0 1px; }",
		".a { width: 100%; max-width: 60ch; height: 100vh; inline-size: 2em; }",
		".a { width: var(--ui-checkbox-box-size, 18px); }",
		".a { top: calc(100% + var(--spacing-xs, 2px)); }",
		".a { font-size: 1.25em; font-size: var(--text-sm, 0.875rem); }",
		".a { line-height: 1; line-height: 0; line-height: var(--leading-snug, 1.4); }",
		".a { letter-spacing: 0; letter-spacing: var(--tracking-wide, 0.04em); }",
		".a { opacity: 0; opacity: 1; opacity: var(--opacity-muted, 0.6); }",
		".a { font-weight: var(--font-weight-semibold, 600); font-weight: inherit; font-weight: bolder; }",
		".a { font-weight: calc(var(--font-weight-semibold, 600) + 50); }",
		".a { font: inherit; font: var(--text-sm, 0.875rem)/var(--leading-snug, 1.4) var(--font-body); }",
		".a { font: 1em/1 monospace; }",
		".a { color: var(--color-text, #111); background: transparent; fill: currentColor; }",
		"@media (max-width: 640px) { .a { display: none; } }",
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

// The scale arms: a spacing, size, type, colour, opacity or shadow value
// written as a literal is a value no theme can reach. Before them, a
// theme could swap every colour and radius and still not move a gap, a
// control's height or a caption's line height.
func TestBareScaleLiteralsAreReported(t *testing.T) {
	for _, tc := range []struct {
		css, want string
	}{
		{".a { padding: 6px 12px; }", "--spacing-"},
		{".a { margin-top: 0.75rem; }", "--spacing-"},
		{".a { column-gap: 12px; }", "--spacing-"},
		{".a { top: 12px; }", "--spacing-* or a --ui-"},
		{".a { inset-inline-end: -6px; }", "--spacing-* or a --ui-"},
		{".a { width: 18px; }", "--ui-<component>-<part>"},
		{".a { max-inline-size: 20rem; }", "--ui-<component>-<part>"},
		{".a { flex-basis: 240px; }", "--ui-<component>-<part>"},
		{".a { font-size: 13px; }", "--text-"},
		{".a { font-size: 0.8125rem; }", "--text-"},
		{".a { line-height: 1.45; }", "--leading-"},
		{".a { line-height: 20px; }", "--leading-"},
		{".a { letter-spacing: 0.02em; }", "--tracking-"},
		{".a { letter-spacing: -1px; }", "--tracking-"},
		{".a { opacity: 0.85; }", "--opacity-"},
		{".a { box-shadow: 0 1px 2px rgba(0,0,0,.05); }", "--shadow-"},
		{".a { box-shadow: 0 4px 12px var(--color-shadow); }", "--shadow-"},
		{".a { box-shadow: 0 1px 0 rgba(0,0,0,.1); }", "--shadow-"},
		{".a { color: #fff; }", "--color-"},
		{".a { background: rgba(0, 0, 0, 0.5); }", "--color-"},
		{".a { border-color: white; }", "--color-"},
		{".a { text-shadow: 0 1px 0 oklch(0.2 0 0); }", "--color-"},
		{".a { border-radius: 0.5rem; }", "--radii-"},
		{".a { border-radius: 62.5rem; }", "--radii-full"},
		{".a { font-weight: 600; }", "--font-weight-"},
		{".a { font-weight: 650; }", "--font-weight-"},
		{".a { font-weight: bold; }", "--font-weight-"},
		{".a { font: 13px/1.4 system-ui; }", "--text-"},
		{".a { font: 13px system-ui; }", "--text-"},
		{".a { font: 600 var(--text-sm, 0.875rem) var(--font-body); }", "--font-weight-"},
		{".a { font: var(--text-sm, 0.875rem)/1.4 var(--font-body); }", "--leading-"},
		{".a { grid-gap: 12px; }", "--spacing-"},
		{".a { scroll-margin-top: 24px; }", "--spacing-"},
		{".a { scroll-padding-inline: 1rem; }", "--spacing-"},
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
	ds := designFixture(t, "battery/admin/x.go",
		"package admin\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".card\").Set(\"padding\", \"10px\").End()\n}\n")
	if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 1 {
		t.Errorf("want 1 GOFASTR1823 on a Set(\"padding\", \"10px\") pair, got %d: %v", len(found), found)
	}
}

// Dev tooling (framework/dev) draws its own chrome, which no app theme
// owns: the scale arms skip it, while the stroke, radius, motion and
// layer arms still apply there.
func TestDevSurfaceSkipsScaleArms(t *testing.T) {
	ds := designFixture(t, "framework/dev/x.go", "package dev\n\nvar css = `.a { padding: 6px; width: 18px; color: #fff; opacity: 0.85; }`\n")
	if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 0 {
		t.Errorf("scale arms fire on a dev surface: %v", found)
	}
	ds = designFixture(t, "framework/dev/x.go", "package dev\n\nvar css = `.a { border: 2px solid #fff; }`\n")
	if found := countRule(t, ds, contracts.RuleBareThemeLiteral); len(found) != 1 {
		t.Errorf("want the stroke arm on a dev surface, got %d: %v", len(found), found)
	}
}
