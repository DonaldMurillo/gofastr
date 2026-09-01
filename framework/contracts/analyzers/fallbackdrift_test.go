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

func TestColourAndNestedFallbacksAreOutOfScope(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { color: var(--color-text, currentColor); border-color: var(--color-border, #2a2b2e); padding: var(--spacing-md, var(--spacing-sm)); width: var(--spacing-md, calc(1px + 2px)); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("colour, nested var() and calc() fallbacks are not the rule's business, got %v", found)
	}
}

func TestFallbackDriftOutsideDesignSystemIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{"main.go": "package main\n\nvar css = `.card { padding: var(--spacing-md, 12px); }`\n"})
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("an app surface is GOFASTR1801's finding, not 1808's, got %v", found)
	}
}
