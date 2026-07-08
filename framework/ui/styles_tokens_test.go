package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The danger button must take its colors from the theme's status
// tokens (with the axe-safe literals as var() fallbacks), not from
// hardcoded hex — a re-themed danger slot has to recolor it.
func TestDangerButtonColorTokens(t *testing.T) {
	css := buttonCSS(style.DefaultTheme())
	i := strings.Index(css, ".ui-button--danger {")
	if i < 0 {
		t.Fatal("ui-button--danger rule missing")
	}
	rule := css[i:]
	rule = rule[:strings.Index(rule, "}")]
	if !strings.Contains(rule, "background: var(--color-danger, #B91C1C)") {
		t.Errorf("danger background must be var(--color-danger, #B91C1C), got rule:\n%s", rule)
	}
	if !strings.Contains(rule, "color: var(--color-primary-fg, #FFFFFF)") {
		t.Errorf("danger text must be var(--color-primary-fg, #FFFFFF), got rule:\n%s", rule)
	}
}

// Same contract for the notification-bell unread badge.
func TestBellBadgeColorTokens(t *testing.T) {
	css := notificationBellCSS(style.DefaultTheme())
	i := strings.Index(css, ".ui-notification-bell__badge {")
	if i < 0 {
		t.Fatal("badge rule missing")
	}
	rule := css[i:]
	rule = rule[:strings.Index(rule, "}")]
	if !strings.Contains(rule, "background: var(--color-danger, #B91C1C)") {
		t.Errorf("badge background must be var(--color-danger, #B91C1C), got rule:\n%s", rule)
	}
	if !strings.Contains(rule, "color: var(--color-primary-fg, #FFFFFF)") {
		t.Errorf("badge text must be var(--color-primary-fg, #FFFFFF), got rule:\n%s", rule)
	}
}

// ButtonSizeLarge must read a DIFFERENT scale token than the default
// size. :root always emits --text-base (1rem), so mapping both base
// and --large onto var(--text-base, …) collapses large into the
// default size regardless of the fallback literal.
func TestButtonLargeUsesTextLgToken(t *testing.T) {
	css := buttonCSS(style.DefaultTheme())
	i := strings.Index(css, ".ui-button--large {")
	if i < 0 {
		t.Fatal("ui-button--large rule missing")
	}
	rule := css[i:]
	rule = rule[:strings.Index(rule, "}")]
	if !strings.Contains(rule, "font-size: var(--text-lg, 1.05rem)") {
		t.Errorf("large button must use var(--text-lg, 1.05rem), got rule:\n%s", rule)
	}
	if strings.Contains(rule, "font-size: var(--text-base") {
		t.Errorf("large button must not read --text-base (that IS the default size), got rule:\n%s", rule)
	}
}

// fontSizeDeclRe matches every font-size: declaration, capturing the
// value (up to the next ; or }). RE2 has no lookahead, so the value
// is inspected in Go: a var()-led value is the sweep's token-with-
// fallback pattern (var(--text-sm, 0.85rem)) and is NOT counted; any
// other value containing a rem/px literal IS — including literals
// nested inside clamp()/calc() that the old narrow regex missed.
var fontSizeDeclRe = regexp.MustCompile(`(?i)font-size\s*:\s*([^;}]*)`)

// fontShorthandRe matches the font: shorthand. "font-size:" and
// "font-family:" don't match because after "font" comes "-", not ":".
var fontShorthandRe = regexp.MustCompile(`(?i)font\s*:\s*([^;}]*)`)

// sizeLiteralRe matches a rem or px length anywhere in a value.
var sizeLiteralRe = regexp.MustCompile(`\.?[0-9][0-9.]*(rem|px)`)

// countFontSizeLiterals counts hardcoded font-size/shorthand literals
// in one sheet's CSS, excluding var(--token, fallback) declarations.
func countFontSizeLiterals(css string) (int, []string) {
	var hits []string
	for _, m := range fontSizeDeclRe.FindAllStringSubmatch(css, -1) {
		val := strings.TrimSpace(m[1])
		if strings.HasPrefix(val, "var(") {
			continue // token-with-fallback — the sweep's intended pattern
		}
		if sizeLiteralRe.MatchString(val) {
			hits = append(hits, "font-size: "+val)
		}
	}
	for _, m := range fontShorthandRe.FindAllStringSubmatch(css, -1) {
		val := strings.TrimSpace(m[1])
		if sizeLiteralRe.MatchString(val) {
			hits = append(hits, "font: "+val)
		}
	}
	return len(hits), hits
}

// Regression guard for the 2026-07 token sweep: component CSS reads
// the typography scale (var(--text-*)) rather than hardcoding sizes.
// A handful of deliberate leftovers remain (values >1px away from any
// scale step, e.g. display sizes above --text-3xl); the budget pins
// their count so new hardcoded font-sizes fail here instead of
// accreting silently. If you trip this: use var(--text-<step>, <lit>)
// — or, for a genuinely off-scale size, raise the budget with a
// comment saying why.
func TestFontSizeLiteralBudget(t *testing.T) {
	// Current leftovers (9 total):
	//   - Fluid clamp() display sizes in ui-hero: clamp(2.5rem, 6vw, 4rem),
	//     clamp(1.125rem, 2.2vw, 1.375rem) — viewport-interpolated, no
	//     single token fits (2).
	//   - Display sizes above --text-3xl: 2.25rem (pricing-card), 1.75rem
	//     (stat-card) (2).
	//   - Micro-labels below --text-xs: 0.625rem (anchored-rail),
	//     0.65rem (avatar-group), 0.68rem (bar-chart) (3).
	//   - Test-registered "hero" button size: 1.15rem (×2 in ui-button) (2).
	const budget = 9
	theme := style.DefaultTheme()
	total := 0
	for _, e := range registry.All() {
		css := e.CSSFor(theme)
		n, hits := countFontSizeLiterals(css)
		if n > 0 {
			total += n
			t.Logf("%s: %d literal font-size decl(s): %v", e.Name, n, hits)
		}
	}
	if total > budget {
		t.Errorf("literal font-size declarations across registered sheets = %d, budget = %d — use var(--text-*) tokens (see log for offenders)", total, budget)
	}
	t.Logf("TOTAL literal font-size declarations: %d", total)
}
