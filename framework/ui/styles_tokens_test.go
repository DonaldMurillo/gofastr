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

// fontSizeLiteralRe matches a font-size declaration whose value is a
// bare rem/px literal instead of a var(--text-*) token reference.
// em-values are excluded: they are parent-relative by design
// (markdown, superscripts) and have no absolute scale step.
var fontSizeLiteralRe = regexp.MustCompile(`font-size\s*:\s*\.?[0-9][0-9.]*(rem|px)`)

// Regression guard for the 2026-07 token sweep: component CSS reads
// the typography scale (var(--text-*)) rather than hardcoding sizes.
// A handful of deliberate leftovers remain (values >1px away from any
// scale step, e.g. display sizes above --text-3xl); the budget pins
// their count so new hardcoded font-sizes fail here instead of
// accreting silently. If you trip this: use var(--text-<step>, <lit>)
// — or, for a genuinely off-scale size, raise the budget with a
// comment saying why.
func TestFontSizeLiteralBudget(t *testing.T) {
	// Current leftovers: display sizes above --text-3xl (2.25rem,
	// 1.75rem), micro-labels below --text-xs (0.625/0.65/0.68rem),
	// and the test-registered "hero" button size (1.15rem).
	const budget = 8
	theme := style.DefaultTheme()
	total := 0
	for _, e := range registry.All() {
		css := e.CSSFor(theme)
		if hits := fontSizeLiteralRe.FindAllString(css, -1); len(hits) > 0 {
			total += len(hits)
			t.Logf("%s: %d literal font-size decl(s): %v", e.Name, len(hits), hits)
		}
	}
	if total > budget {
		t.Errorf("literal font-size declarations across registered sheets = %d, budget = %d — use var(--text-*) tokens (see log for offenders)", total, budget)
	}
}
