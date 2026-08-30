package check

// RED security tests from the pass-4 adversarial review (findings
// CHECK-R1..R4). They pin the CSP gate's documented contract:
// LintNoInlineScripts "reports any [string literal] that contains a
// <script>…</script> block whose opening tag lacks a src= attribute" —
// evaluated against what the literal COMPILES to and what a BROWSER
// executes, not what the raw source text happens to spell. Each
// property below fails on the current implementation; they turn green
// when the linter (a) resolves escape sequences before scanning,
// (b) anchors attribute names so data-src/data-type do not count as
// src/type, (c) classifies every <script> tag in a literal instead of
// only the first, and (d) matches HTML tag/attribute names
// case-insensitively.

import (
	"testing"
)

// CHECK-R1: scanInlineScripts (noinlinescripts.go:186) scans
// stripStringLiteral(node.Value) — raw source text, escapes left
// spelled out as backslash sequences. The style linter was already
// fixed for this exact class (noinlinestyles.go:139-146 resolves
// escapes via strconv.Unquote and documents why); the script path was
// not. Property: a string literal whose compiled value contains an
// inline <script> block must produce a violation.
func TestSecurityScriptsEscapedLiteralBypass(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			// Control: the gate works on the unescaped form.
			name: "plain literal is flagged",
			body: "package x\nvar s = \"<script>alert(1)</script>\"\n",
		},
		{
			// Compiles to <script>alert(1)</script>.
			name: "hex-escaped tags",
			body: "package x\nvar s = \"\\x3cscript>alert(1)\\x3c/script>\"\n",
		},
		{
			// Compiles to <script>alert(1)</script>.
			name: "unicode-escaped tags",
			body: "package x\nvar s = \"\\u003c\\u0073cript>alert(1)\\u003c\\u0073cript>\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, "fixture.go", tc.body)
			res, err := LintNoInlineScripts(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !res.HasErrors() {
				t.Errorf("CSP gate passed, but this literal compiles to <script>alert(1)</script> — a live inline script under default-src 'self'.\nfixture source: %q\nviolations: %v", tc.body, res.Violations)
			}
		})
	}
}

// CHECK-R2: scriptSrcRe / scriptInertTypeRe (noinlinescripts.go:59,63)
// place \b before src/type, and \b matches between '-' and a letter, so
// the tail of a hyphenated attribute name (data-src=, data-type=,
// ng-src=, …) is treated as the real attribute. The browser ignores
// those attribute names and executes the body. Property: only an actual
// src=/type= attribute may classify a script as external/inert.
func TestSecurityScriptsAttrNameAnchoring(t *testing.T) {
	cases := []struct {
		name    string
		lit     string
		wantBad bool
	}{
		{
			name:    "real src attribute is external (control)",
			lit:     `<script src="/a.js"></script>`,
			wantBad: false,
		},
		{
			name:    "real inert type attribute is allowed (control)",
			lit:     `<script type="application/json">{"a":1}</script>`,
			wantBad: false,
		},
		{
			name:    "data-src is not the src attribute",
			lit:     `<script data-src="/a.js">alert(1)</script>`,
			wantBad: true,
		},
		{
			name:    "data-type is not the type attribute",
			lit:     `<script data-type="application/json">alert(1)</script>`,
			wantBad: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &Result{}
			checkInlineScriptInString(tc.lit, 1, "fixture.go", res)
			if got := res.HasErrors(); got != tc.wantBad {
				t.Errorf("violations = %v, want violation=%v for %q", res.Violations, tc.wantBad, tc.lit)
			}
		})
	}
}

// CHECK-R3: checkInlineScriptInString (noinlinescripts.go:217-230)
// classifies only the FIRST <script> tag in a literal and returns as
// soon as tag #1 carries src= or an inert type, so a leading
// external/inert tag masks a later executable block in the same
// literal. Property: every <script> tag in a literal must be
// classified; an inline block anywhere means a violation.
func TestSecurityScriptsAllTagsClassified(t *testing.T) {
	cases := []struct {
		name string
		lit  string
	}{
		{
			name: "external first tag must not mask inline second tag",
			lit:  "<script src=\"/a.js\"></script>\n<script>alert(1)</script>",
		},
		{
			name: "inert first tag must not mask inline second tag",
			lit:  "<script type=\"application/json\">{\"a\":1}</script>\n<script>alert(1)</script>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &Result{}
			checkInlineScriptInString(tc.lit, 1, "fixture.go", res)
			if !res.HasErrors() {
				t.Errorf("the second <script>alert(1)</script> block is executable inline JS but was not reported (violations: %v) for %q", res.Violations, tc.lit)
			}
		})
	}
}

// CHECK-R4: scriptOpenRe/scriptCloseRe (noinlinescripts.go:57,68) and
// inlineStyleAttrRe (noinlinestyles.go:60) compile case-sensitively —
// scriptInertTypeRe carries (?si), so case-insensitivity was
// considered and not applied to the tag/attribute-name patterns. HTML
// tag and attribute names are ASCII case-insensitive to browsers.
// Property: markup the browser executes/applies must be flagged
// regardless of case. Covers both linter halves of the finding.
func TestSecurityMarkupCaseInsensitive(t *testing.T) {
	t.Run("uppercase script tags execute in browsers", func(t *testing.T) {
		res := &Result{}
		checkInlineScriptInString("<SCRIPT>alert(1)</SCRIPT>", 1, "fixture.go", res)
		if !res.HasErrors() {
			t.Error("browsers execute <SCRIPT> bodies; the script linter must flag them (got zero violations)")
		}
	})
	t.Run("uppercase STYLE attribute applies in browsers", func(t *testing.T) {
		res := &Result{}
		checkInlineStyleInString(`<div STYLE="color:red">x</div>`, 1, "fixture.go", res)
		if !res.HasErrors() {
			t.Error("browsers apply STYLE= attributes; the style linter must flag them (got zero violations)")
		}
	})
}
