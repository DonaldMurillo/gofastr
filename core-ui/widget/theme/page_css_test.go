package theme_test

// Issue #136 audit slice: Hard rule 7 probes. "Setting a CSS property
// where a var(--*) token belongs" is a listed STOP tripwire. These
// guard the places PageCSS used to write a literal that exactly (or
// near-exactly) matched an existing canonical token; they were RED
// until the literals were swapped for token references.
//
// Why it matters, concretely: the package doc promises "a single token
// swap re-skins the whole app" (PageTheme comment). A hardcoded literal
// breaks that promise — swapping --shadow-sm or --text-xs leaves these
// rules behind, and dark-mode/brand overrides cannot reach them.

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget/theme"
)

// ruleBlock extracts the declarations of the first CSS rule whose
// selector line contains sel.
func ruleBlock(t *testing.T, css, sel string) string {
	t.Helper()
	idx := strings.Index(css, sel)
	if idx < 0 {
		t.Fatalf("selector %q not found in PageCSS output", sel)
	}
	end := strings.Index(css[idx:], "}")
	if end < 0 {
		t.Fatalf("rule for %q has no closing brace", sel)
	}
	return css[idx : idx+end]
}

func TestPageCSS_CardShadowMustUseShadowToken(t *testing.T) {
	block := ruleBlock(t, theme.PageCSS(theme.PageTheme()), ".kiln-card {")
	if !strings.Contains(block, "box-shadow: var(--shadow-") {
		t.Errorf("AUDIT FINDING (hard rule 7): .kiln-card hardcodes its box-shadow (%q); the canonical --shadow-sm token (0 1px 2px 0 rgba(0,0,0,0.05), near-identical) exists and belongs there", strings.TrimSpace(block))
	}
}

func TestPageCSS_TypographySizesMustUseTextTokens(t *testing.T) {
	// .kiln-eyebrow writes 0.75rem where --text-xs is exactly 0.75rem.
	eyebrow := ruleBlock(t, theme.PageCSS(theme.PageTheme()), ".kiln-eyebrow {")
	if !strings.Contains(eyebrow, "font-size: var(--text-xs)") {
		t.Errorf("AUDIT FINDING (hard rule 7): .kiln-eyebrow hardcodes font-size 0.75rem; --text-xs has the exact same value")
	}
	// .kiln-h2 writes 1.5rem where --text-2xl is exactly 1.5rem.
	h2 := ruleBlock(t, theme.PageCSS(theme.PageTheme()), ".kiln-h2 {")
	if !strings.Contains(h2, "font-size: var(--text-2xl)") {
		t.Errorf("AUDIT FINDING (hard rule 7): .kiln-h2 hardcodes font-size 1.5rem; --text-2xl has the exact same value")
	}
	// body base typography writes 16px where --text-base is 1rem.
	body := ruleBlock(t, theme.PageCSS(theme.PageTheme()), "body.kiln-app {")
	if !strings.Contains(body, "font-size: var(--text-base)") {
		t.Errorf("AUDIT FINDING (hard rule 7): body.kiln-app hardcodes font-size 16px; --text-base (1rem) belongs there")
	}
}
