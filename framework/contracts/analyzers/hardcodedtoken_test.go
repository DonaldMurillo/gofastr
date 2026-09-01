package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1807: design-system CSS that sets a property to a literal exactly
// equal to a theme token's value. The inverse of GOFASTR1806: that rule
// validates var(--x) references against declared tokens, this one catches
// the value bypassing the token entirely. core-ui/widget/theme carried
// `font-size: 0.75rem` beside `--text-xs: 0.75rem` for months; the pixels
// are identical until someone re-themes, and then the rule silently stays
// behind.
//
// The rule fires only inside the design-system trees
// (core-ui/, framework/ui/, framework/uihost/, framework/sdkdocs/,
// framework/pluginhost/, framework/dev/, battery/): everywhere else any
// CSS at all is GOFASTR1801's finding, and this rule would only add a
// second diagnostic for the same line.

// designFixture runs the analyzers over a module whose one file lives at
// rel, which must sit under a design-system prefix or the rule is not
// expected to run at all.
func designFixture(t *testing.T, rel, body string) []contracts.Diagnostic {
	t.Helper()
	return fixture(t, map[string]string{rel: body})
}

func TestHardcodedTokenValueInCSSStringIsReported(t *testing.T) {
	ds := designFixture(t, "core-ui/style/page.go",
		"package style\n\nvar css = `.eyebrow { font-size: 0.75rem; }`\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 GOFASTR1807, got %d: %v", len(found), found)
	}
	d := found[0]
	if !strings.Contains(d.Message, "text-xs") {
		t.Errorf("message does not name the token: %q", d.Message)
	}
	if !strings.Contains(d.Message, "var(--text-xs)") {
		t.Errorf("message does not show the reference to write: %q", d.Message)
	}
	if d.File != "core-ui/style/page.go" || d.Line != 3 {
		t.Errorf("finding at %s:%d, want core-ui/style/page.go:3", d.File, d.Line)
	}
}

func TestHardcodedTokenValueInSetPairIsReported(t *testing.T) {
	// The Set("prop", "value") builder shape carries the property and the
	// value in two string literals; the colon-shaped CSS regex cannot see
	// it. This is the shape core-ui/widget/theme/page.go actually used.
	ds := designFixture(t, "core-ui/widget/page.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".card\").Set(\"gap\", \"8px\").End()\n\tss.Rule(\".card\").Set(\"padding-top\", \"16px\").End()\n}\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 2 {
		t.Fatalf("want exactly 2 GOFASTR1807 (gap 8px = --spacing-md, padding-top 16px = --spacing-lg), got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "spacing-md") || !strings.Contains(found[1].Message, "spacing-lg") {
		t.Errorf("messages do not name their tokens: %q / %q", found[0].Message, found[1].Message)
	}
	if found[0].Line != 4 || found[1].Line != 5 {
		t.Errorf("findings at lines %d and %d, want 4 and 5", found[0].Line, found[1].Line)
	}
}

func TestPairFiresOncePerValueNotPerLine(t *testing.T) {
	// Several prop/value pairs share one Set call line; each value is
	// judged on its own, and font-weight (no token category) stays quiet.
	ds := designFixture(t, "core-ui/widget/page.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".h2\").Set(\"font-size\", \"1.5rem\", \"font-weight\", \"700\").End()\n}\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 GOFASTR1807, got %d: %v", len(found), found)
	}
	if !strings.Contains(found[0].Message, "text-2xl") {
		t.Errorf("message does not name --text-2xl: %q", found[0].Message)
	}
}

func TestTokenDeclarationItselfIsClean(t *testing.T) {
	// `--text-xs: 0.75rem` in a :root block is a token STATING its value;
	// demanding a var() there would be a loop. The property-name
	// allowlist keeps custom-property declarations out (no listed
	// property name matches `--text-xs`).
	ds := designFixture(t, "core-ui/style/root.go",
		"package style\n\nvar root = `:root { --text-xs: 0.75rem; --spacing-md: 8px; }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a token declaration must state its value")
}

func TestVarReferenceIsClean(t *testing.T) {
	// A var() value is a reference chain, not a literal — including the
	// one case where it EQUALS a token's value: tk-pn declares
	// var(--color-code-text) as its value, so writing exactly that must
	// not be reported as hardcoding tk-pn. The fallback form
	// (var(--spacing-md, 8px)) restates the token value for degraded
	// mode and is the encouraged shape, in both declaration shapes.
	ds := designFixture(t, "core-ui/style/sheet.go",
		"package style\n\nvar css = `.code .pn { color: var(--color-code-text); } .card { gap: var(--spacing-md, 8px); }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a var() reference is the token chain, not a literal duplicate")
	ds = designFixture(t, "core-ui/widget/sheet.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".card\").Set(\"gap\", \"var(--spacing-md, 8px)\").End()\n}\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a builder var() fallback is the degraded-mode copy, not a bypass")
}

func TestBareKeywordValueIsClean(t *testing.T) {
	// `none` is shadow-none's value, but `box-shadow: none` is idiomatic
	// CSS for "no shadow", not a token bypass; a bare keyword carries no
	// evidence. Values without a digit, hex, quote, comma, or paren never
	// enter the index, so keyword values cannot fire at all.
	ds := designFixture(t, "core-ui/style/none.go",
		"package style\n\nvar css = `.list { box-shadow: none; }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a bare CSS keyword is not a token bypass")
}

func TestOffScaleValueIsClean(t *testing.T) {
	// Values no token carries are the MISSING-token finding (add it
	// upstream), not this rule. Includes near-misses: 999px is not
	// --radii-full's 9999px, and equality is the whole test.
	ds := designFixture(t, "core-ui/style/off.go",
		"package style\n\nvar css = `.x { font-size: 0.72rem; } .pill { border-radius: 999px; } .y { gap: 6px; }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "off-scale values have no token to duplicate")
}

func TestShorthandWithValueComponentIsClean(t *testing.T) {
	// `padding: 8px 12px` contains a token value as a COMPONENT; the rule
	// judges the whole value, and the shorthand does not equal any token.
	ds := designFixture(t, "core-ui/style/shorthand.go",
		"package style\n\nvar css = `.field { padding: 8px 12px; margin: 0 auto; }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a shorthand with a token-sized component is not a whole-value duplicate")
}

func TestBuilderTokenRefIsClean(t *testing.T) {
	// `{text.xs}` and `{spacing.md}` ARE token references in the
	// StyleSheet builder; resolving them to var(--text-xs) is the rule's
	// documented fix, not a finding.
	ds := designFixture(t, "core-ui/widget/ref.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".card\").Set(\"font-size\", \"{text.xs}\", \"gap\", \"{spacing.md}\").End()\n}\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "a builder token reference is the fix, not the bug")
}

func TestAppSurfaceIsClean(t *testing.T) {
	// Outside the design-system trees the same literal is not this rule's
	// finding: any CSS there is GOFASTR1801's. Asserting 1801 fires on
	// the same line proves the analyzers ran and saw it (absence of 1807
	// is then a decision, not a blind spot).
	ds := fixture(t, map[string]string{
		"main.go": "package main\n\nvar css = `.card { font-size: 0.75rem; }`\n",
	})
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "an app surface is GOFASTR1801's finding, not this rule's")
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestMediaQueryBreakpointIsClean(t *testing.T) {
	// breakpoint tokens exist (640/768/1024/…) but a media query cannot
	// read a custom property: var() is illegal in @media conditions, so
	// hardcoding the px there is forced by CSS. The breakpoint category
	// never enters the property map.
	ds := designFixture(t, "core-ui/style/media.go",
		"package style\n\nvar q = ss.Media(\"(min-width: 768px)\", nil)\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "media queries cannot read var(); breakpoint px are forced")
}

func TestZIndexTokenValueIsReported(t *testing.T) {
	// z-index keeps its category: a var() is legal there, and 300 is
	// exactly --z-modal. The values are bare round numbers, so this is the
	// rule's most coincidence-prone category; it stays because writing a
	// design-system layering value that a z token already names is the
	// same drift as the font-size case.
	ds := designFixture(t, "core-ui/style/overlay.go",
		"package style\n\nvar css = `.overlay { z-index: 300; }`\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 || !strings.Contains(found[0].Message, "z-modal") {
		t.Fatalf("want 1 GOFASTR1807 naming z-modal, got %d: %v", len(found), found)
	}
}

func TestHexValueComparesCaseInsensitively(t *testing.T) {
	// Hex colours fold case in CSS: #ffffff and #FFFFFF are the same
	// colour, and the theme declares --color-surface: #FFFFFF. A
	// case-sensitive compare would let a lowercase literal through.
	ds := designFixture(t, "core-ui/style/hex.go",
		"package style\n\nvar css = `.chip { background: #ffffff; }`\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 || !strings.Contains(found[0].Message, "color-surface") {
		t.Fatalf("want 1 GOFASTR1807 naming color-surface, got %d: %v", len(found), found)
	}
}

func TestImportantSuffixStillCompares(t *testing.T) {
	// `color: #B91C1C !important` duplicates --color-danger; the
	// !important flag is orthogonal to the value and must not mask it.
	ds := designFixture(t, "core-ui/style/imp.go",
		"package style\n\nvar css = `.alert { color: #B91C1C !important; }`\n")
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 || !strings.Contains(found[0].Message, "color-danger") {
		t.Fatalf("want 1 GOFASTR1807 naming color-danger, got %d: %v", len(found), found)
	}
}

func TestHardcodedTokenValueAllowDirectiveSuppresses(t *testing.T) {
	// The escape hatch: a directive with a reason, covering the next code
	// line. Without it the finding must still appear, so the directive is
	// a decision rather than an exemption for the whole file type.
	ds := designFixture(t, "core-ui/style/knob.go",
		"package style\n\n//gofastr:allow(GOFASTR1807) the knob is deliberately theme-invariant across re-skins\nvar css = `.knob { font-size: 0.75rem; }`\n")
	assertNot(t, ds, contracts.RuleHardcodedTokenValue, "an allow directive with a reason covers the next line")

	ds = designFixture(t, "core-ui/style/knob2.go",
		"package style\n\nvar css = `.knob { font-size: 0.75rem; }`\n")
	if len(countRule(t, ds, contracts.RuleHardcodedTokenValue)) != 1 {
		t.Fatal("absence of a directive must keep the finding")
	}
}
