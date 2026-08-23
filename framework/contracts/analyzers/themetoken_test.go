package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1806: a `var(--…)` reference in project CSS that the theme does
// not emit. Issue #214's reporter wrote `--radius-lg` where the theme
// emits `--radii-lg`; an invalid var() is not a CSS error, the
// declaration is silently dropped, and the only symptom was every
// rounded corner rendering square for days.

func countRule(t *testing.T, ds []contracts.Diagnostic, rule string) []contracts.Diagnostic {
	t.Helper()
	var out []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == rule {
			out = append(out, d)
		}
	}
	return out
}

func TestUnknownThemeTokenIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".card { border-radius: var(--radius-lg); }\n",
	})
	found := countRule(t, ds, contracts.RuleUnknownThemeToken)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 GOFASTR1806, got %d: %v", len(found), found)
	}
	d := found[0]
	if !strings.Contains(d.Message, "radius-lg") {
		t.Errorf("message does not name the unknown token: %q", d.Message)
	}
	if !strings.Contains(d.Message, "radii-lg") {
		t.Errorf("message does not suggest the close match: %q", d.Message)
	}
	if d.File != "app.css" || d.Line != 1 {
		t.Errorf("finding at %s:%d, want app.css:1", d.File, d.Line)
	}
}

func TestKnownThemeTokenIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".card { border-radius: var(--radii-lg); color: var(--color-text); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "radii-lg and color-text are emitted by the theme")
}

func TestVarWithFallbackIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".card { border-radius: var(--radius-lg, 8px); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "a fallback degrades instead of dropping the declaration")
}

func TestLocallyDeclaredPropertyIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ":root { --brand: #fff; }\n.header { color: var(--brand); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "--brand is declared by the stylesheet itself")
}

func TestPropertyDeclaredInAnotherFileIsClean(t *testing.T) {
	// The declared set is built across all style files: a base sheet
	// declaring a custom property another sheet uses is normal.
	ds := fixture(t, map[string]string{
		"base.css": ":root { --brand: #fff; }\n",
		"app.css":  ".header { color: var(--brand); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "--brand is declared in base.css")
}

func TestUiKnobReferenceIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".shell { max-width: var(--ui-layout-container-width); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "ui-* knobs are per-component overrides, not theme tokens")
}

func TestCSSFileDoesNotTripGoRules(t *testing.T) {
	// A stylesheet is not an app shipping CSS from Go. It must not fire
	// the bespoke-CSS rule; the file IS a stylesheet, outside the design
	// system or not, and GOFASTR1806 is the rule that governs it.
	ds := fixture(t, map[string]string{
		"app.css": ".card { padding: 16px; border-radius: 8px; color: #fff; }\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a stylesheet is not Go source carrying CSS in strings")
}

func TestNestedFallbackIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".card { border-radius: var(--nope, var(--radii-lg)); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "--nope carries a nested fallback, so nothing is dropped")
}

func TestTokenInCSSCommentIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": "/* do not use var(--radius-lg), it is a typo */\n.card { margin: 0 }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "prose in a comment is not a reference")
}

// Text inside a CSS string is content, not a token reference. Nothing
// is dropped, so an Error here would fail a build on valid CSS.
func TestTokenInCSSStringIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x::after { content: "var(--not-a-token)"; }` + "\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "text inside a CSS string is content, not a reference")
}

// A `/*` inside a string used to open a comment that never closed,
// swallowing the rest of the file and hiding a real typo on a later
// line. The line number is asserted because preserving it is the whole
// risk of blanking spans in the pre-pass.
func TestTokenAfterStringWithOpenComment(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x::after { content: "/*"; }
.y { color: var(--radius-lg); }
`,
	})
	found := countRule(t, ds, contracts.RuleUnknownThemeToken)
	if len(found) != 1 {
		t.Fatalf("want exactly 1 GOFASTR1806, got %d: %v", len(found), found)
	}
	if found[0].File != "app.css" || found[0].Line != 2 {
		t.Errorf("finding at %s:%d, want app.css:2", found[0].File, found[0].Line)
	}
}

// Both regex scans run on the blanked text: a declaration after a
// string must still count, and the reference it satisfies stays clean.
func TestDeclarationAfterStringStillCounts(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x::after { content: "/*"; }
:root { --brand: #fff; }
.a { color: var(--brand); }
`,
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "--brand is declared on line 2, after the string")
}

func TestEscapedQuoteDoesNotEndString(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x::after { content: "a\"var(--nope)b"; }` + "\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "the escaped quote does not end the string")
}

func TestSingleQuotedStringIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x::after { content: 'var(--not-a-token)'; }` + "\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "single-quoted strings are strings too")
}

// The case a naive "blank from the first quote to end of line" fix
// would break: a real reference after a closed string on the same line.
func TestReferenceAfterClosedStringReports(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": `.x { content: "hi"; color: var(--radius-lg); }` + "\n",
	})
	found := countRule(t, ds, contracts.RuleUnknownThemeToken)
	if len(found) != 1 || found[0].Line != 1 {
		t.Fatalf("want exactly 1 GOFASTR1806 at app.css:1, got %d: %v", len(found), found)
	}
}

// A stylesheet is the one place GOFASTR1806 fires, so it has to be a
// place a suppression can be written. collectSuppressions read Go files
// only, which left the rule with no escape hatch in the file it reports.
func TestCSSAllowDirectiveSuppresses(t *testing.T) {
	ds := fixture(t, map[string]string{
		"static/app.css": "/* gofastr:allow(GOFASTR1806) --brand-x is set inline by the host page */\n.a { color: var(--brand-x); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "an allow directive in the stylesheet must waive it")
}

// The file-scoped hammer works in CSS too.
func TestCSSAllowFileDirective(t *testing.T) {
	ds := fixture(t, map[string]string{
		"static/app.css": "/* gofastr:allow-file(GOFASTR1806) vendored third-party sheet */\n.a { color: var(--brand-x); }\n.b { gap: var(--brand-y); }\n",
	})
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "an allow-file directive in the stylesheet must waive it")
}

// Absence of a directive still reports: the escape hatch must not have
// widened into "stylesheets are exempt".
func TestCSSWithoutDirectiveStillReports(t *testing.T) {
	ds := fixture(t, map[string]string{
		"static/app.css": ".a { color: var(--brand-x); }\n",
	})
	assertHas(t, ds, contracts.RuleUnknownThemeToken)
}
