package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1808: a var() fallback in design-system CSS that restates a
// length- or time-scale token at a value the theme does not declare.
// Issue #365: ~450 fallbacks taught a 4/8/16/24/32 spacing ladder while
// the theme declares 2/4/8/16/24; themed pages rendered the declared
// values all along, so the fallbacks were the only place the wrong scale
// existed, and a reviewer read one as the token's value.

func TestFallbackDriftInCSSStringIsReported(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { padding: var(--spacing-md, 12px); }`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 GOFASTR1808, got %d: %v", len(found), found)
	}
	d := found[0]
	for _, want := range []string{"--spacing-md", "12px", "8px", "var(--spacing-md, 8px)"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message lacks %q: %q", want, d.Message)
		}
	}
	if d.File != "framework/ui/card.go" || d.Line != 3 {
		t.Errorf("finding at %s:%d, want framework/ui/card.go:3", d.File, d.Line)
	}
}

func TestFallbackDriftInSetPairIsReported(t *testing.T) {
	ds := designFixture(t, "core-ui/widget/page.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".x\").Set(\"font-size\", \"var(--text-sm, 0.85rem)\").End()\n}\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 || !strings.Contains(found[0].Message, "0.875rem") {
		t.Fatalf("want 1 GOFASTR1808 naming 0.875rem, got %v", found)
	}
}

func TestFallbackMatchingTokenIsQuiet(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { padding: var(--spacing-md, 8px); gap: var(--spacing-lg, 1rem); transition: all var(--duration-fast, .15s); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("declared and unit-equivalent fallbacks must be quiet, got %v", found)
	}
}

func TestRemFallbackOffByAStepIsReported(t *testing.T) {
	// 1.5rem is 24px; --spacing-lg is 16px. The unit conversion must not
	// turn every rem fallback into a pass.
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { gap: var(--spacing-lg, 1.5rem); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 1 {
		t.Fatalf("want 1 GOFASTR1808 for 1.5rem vs 16px, got %v", found)
	}
}

func TestKeywordAndNestedFallbacksAreQuiet(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { color: var(--color-text, currentColor); background: var(--color-surface, transparent); outline-color: var(--color-text, var(--fui-ink)); padding: var(--spacing-md, var(--spacing-sm)); width: var(--spacing-md, calc(1px + 2px)); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("keyword, nested var() and calc() fallbacks are not the rule's business, got %v", found)
	}
}

// Colour fallbacks drifted the same way: ~340 still named the palette
// from before the AA pass (var(--color-danger, #DC2626) where the theme
// declares #B91C1C), so a reviewer read the old red as the token's.

func TestColourFallbackDriftIsReported(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { color: var(--color-danger, #DC2626); }`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 || !strings.Contains(found[0].Message, "var(--color-danger, #B91C1C)") {
		t.Fatalf("want 1 GOFASTR1808 naming #B91C1C, got %v", found)
	}
}

func TestColourFallbackMatchingSchemeIsQuiet(t *testing.T) {
	// The light value in any hex case or the short form; inside a
	// dark-scheme block, a one-line dark rule or a dark @media, the dark
	// value. The block ends where its braces balance.
	ds := designFixture(t, "framework/ui/card.go", "package ui\n\nvar css = `"+
		".card { color: var(--color-danger, #B91C1C); fill: var(--color-danger, #b91c1c); background: var(--color-surface, #fff); }\n"+
		"[data-color-scheme=\"dark\"] .card { color: var(--color-danger, #F87171); }\n"+
		"@media (prefers-color-scheme: dark) {\n"+
		"  .card {\n"+
		"    color: var(--color-danger, #F87171);\n"+
		"  }\n"+
		"}\n"+
		".after { color: var(--color-danger, #B91C1C); }\n"+
		".a{color:var(--color-danger, #B91C1C)} @media (prefers-color-scheme: dark){.a{color:var(--color-danger, #F87171)}} .b{fill:var(--color-danger, #B91C1C)} [data-theme=dark] .b{fill:var(--color-danger, #F87171)}`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("each scheme's own value must be quiet, got %v", found)
	}
}

func TestDarkValueInLightRuleIsReported(t *testing.T) {
	// #A1A1AA is text-subtle's dark value and its old light one, which
	// failed AA on white.
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.eyebrow { color: var(--color-text-subtle, #A1A1AA); }`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 || !strings.Contains(found[0].Message, "var(--color-text-subtle, #71717A)") {
		t.Fatalf("want 1 GOFASTR1808 naming #71717A, got %v", found)
	}
}

func TestLightValueInDarkBlockIsReported(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go", "package ui\n\nvar css = `"+
		"@media (prefers-color-scheme: dark) {\n"+
		"  .card { color: var(--color-danger, #B91C1C); }\n"+
		"}`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 || !strings.Contains(found[0].Message, "var(--color-danger, #F87171)") || found[0].Line != 4 {
		t.Fatalf("want 1 GOFASTR1808 at line 4 naming #F87171, got %v", found)
	}
}

func TestNonHexColourFallbacksAreReported(t *testing.T) {
	// rgba(), a named colour, an alpha hex and code-highlight tokens (tk-pn
	// is declared as a var()): a colour fallback is the declared hex or a
	// keyword.
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { border-color: var(--color-border, rgba(0,0,0,0.1)); color: var(--color-danger, red); fill: var(--color-danger, #B91C1C80); stroke: var(--tk-kw, #FFFFFF); stop-color: var(--tk-pn, red); }`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 5 {
		t.Fatalf("want 5 GOFASTR1808, got %d: %v", len(found), found)
	}
}

func TestDesktopThemeColourFallbacksAreQuiet(t *testing.T) {
	// battery/desktop/ui ships its own macOS theme; its fallbacks follow
	// that palette, not the framework's.
	ds := designFixture(t, "battery/desktop/ui/frame.go",
		"package ui\n\nvar css = `.frame { color: var(--color-text, #F5F5F7); padding: var(--spacing-md, 12px); }`\n")
	found := countRule(t, ds, contracts.RuleFallbackDrift)
	if len(found) != 1 || !strings.Contains(found[0].Message, "--spacing-md") {
		t.Fatalf("want only the spacing finding, got %v", found)
	}
}

func TestFallbackDriftOutsideDesignSystemIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": "package main\n\nvar css = `.card { padding: var(--spacing-md, 12px); }`\n"})
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("an app surface is GOFASTR1801's finding, not 1808's, got %v", found)
	}
}

func TestUnknownColourTokenFallbackIsQuiet(t *testing.T) {
	// A name the theme does not declare is GOFASTR1806's finding; there is
	// no declared value to drift from.
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { color: var(--color-brand, #123456); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("an undeclared colour token has nothing to drift from, got %v", found)
	}
}

func TestSplicedColourFallbackIsQuiet(t *testing.T) {
	// A fallback built in Go (a theme preset splicing its own ink) is
	// code, not a CSS literal.
	ds := designFixture(t, "framework/ui/theme/brutal.go",
		"package theme\n\nfunc hard(px, ink string) string { return px + \" 0 var(--color-border, \" + ink + \")\" }\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("a Go-spliced fallback is not a CSS value, got %v", found)
	}
}
