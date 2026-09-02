package check

// Issue #136 audit slice: probes proving each check can actually fail.
// Each test asserts the rule FIRES on the violating input (or stays quiet
// on the legitimate one), so a finding shows up as a red test and stays
// pinned once fixed. The data-src and uppercase <SCRIPT> probes this file
// once carried are pinned by noinlinescripts_security_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── FINDING: style regex + attrs-key match are case-sensitive ─────────────
//
// `STYLE="color:red"` is stripped by the browser under the same strict CSP
// as lowercase, and html attribute maps with a "Style" key emit the same
// attribute. Both spellings pass the linter.
func TestProbe_UppercaseStyleAttrIsMissed(t *testing.T) {
	dir := writeStyleFixture(t, `
var x = "<div STYLE=\"color:red\">hi</div>"
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Error("AUDIT FINDING: uppercase STYLE=\"…\" attribute passed the inline-style linter (HTML is case-insensitive).")
	}
}

func TestProbe_UppercaseStyleMapKeyIsMissed(t *testing.T) {
	dir := writeStyleFixture(t, `
var x = map[string]string{"Style": "color:red"}
`)
	res, err := LintNoInlineStyles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Error("AUDIT FINDING: attrs key \"Style\" (HTML attribute names are case-insensitive) passed the inline-style linter.")
	}
}

// ── FINDING: novarjs blanks template-literal interpolations ───────────────
//
// stripJSCommentsAndStrings blanks the whole backtick literal including
// ${…} interpolations, but interpolation bodies are EXECUTABLE JS. A `var`
// declaration inside one is real code the lint cannot see.
func TestProbe_VarInsideTemplateInterpolationIsMissed(t *testing.T) {
	dir := t.TempDir()
	body := "const s = `${(function(){ var sneaky = 1; return sneaky })()}`;\n"
	if err := os.WriteFile(filepath.Join(dir, "tpl.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() {
		t.Error("AUDIT FINDING: `var` inside a template-literal ${…} interpolation (executable code) passed the no-var lint.")
	}
}

// ── FINDING (opposite direction): regex literals false-positive ───────────
//
// stripJSCommentsAndStrings has no regex-literal state, so `/var/` (or any
// regex containing the word var) survives sanitization as code and trips
// the keyword scan. A runtime module legitimately matching the string
// "var" (e.g. a minifier detector) cannot pass the lint.
func TestProbe_VarInsideRegexLiteralFalsePositives(t *testing.T) {
	dir := t.TempDir()
	body := "const isVarDecl = /var\\s+\\w+/.test(line);\n"
	if err := os.WriteFile(filepath.Join(dir, "re.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("AUDIT FINDING (false positive): regex literal /var…/ is flagged as a var declaration:\n%s", res.Error())
	}
}

// ── Refutations: rules that DO fire on their violating input ──────────────

func TestProbe_TypeSwitchIsFlagged(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "typeswitch.ui.go", `package test
func F(v any) {
	switch x := v.(type) {
	case int:
		_ = x
	}
}
`)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, v := range result.Violations {
		if strings.Contains(v.Message, "type switches not allowed") {
			found = true
		}
	}
	if !found {
		t.Errorf("type-switch ban did not fire on a violating input; got: %v", result.Violations)
	}
}

func TestProbe_CloseAndRecoverAreFlagged(t *testing.T) {
	dir := t.TempDir()
	path := writeTempGoFile(t, dir, "builtins.ui.go", `package test
func F(ch chan int) {
	close(ch)
}
func G() {
	defer func() { _ = recover() }()
}
`)
	result, err := LintFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var gotClose, gotRecover bool
	for _, v := range result.Violations {
		if strings.Contains(v.Message, "channel close not allowed") {
			gotClose = true
		}
		if strings.Contains(v.Message, "recover not allowed") {
			gotRecover = true
		}
	}
	if !gotClose {
		t.Errorf("close() ban did not fire; got: %v", result.Violations)
	}
	if !gotRecover {
		t.Errorf("recover() ban did not fire; got: %v", result.Violations)
	}
}

// The regex-literal fix must not swallow division: a `/` after a value
// divides, and a `var` after it is still a declaration.
func TestProbe_DivisionDoesNotHideVar(t *testing.T) {
	dir := t.TempDir()
	body := "const avg = total / count;\nvar leak = 1;\nconst r = (a) / 2 / b;\nvar leak2 = 2;\n"
	if err := os.WriteFile(filepath.Join(dir, "div.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() || !strings.Contains(res.Error(), "div.js:2:") || !strings.Contains(res.Error(), "div.js:4:") {
		t.Fatalf("both var declarations after a division must be flagged, got: %v", res)
	}
}

// A regex after a keyword, in a character class, or with flags is still
// a pattern; none of these spell a declaration.
func TestProbe_RegexAfterKeywordAndInClassIsQuiet(t *testing.T) {
	dir := t.TempDir()
	body := "function f(s) { return /var x/.test(s) || /[/]var/gi.test(s); }\nconst k = typeof /var/;\n"
	if err := os.WriteFile(filepath.Join(dir, "re2.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Fatalf("regex literals flagged as declarations: %v", res.Error())
	}
}

// An escape that swallows a newline inside a regex literal must keep the
// newline in the sanitized stream: line numbers come from that stream.
func TestRegexEscapedNewlineKeepsLineNumbers(t *testing.T) {
	dir := t.TempDir()
	body := "const r = /a\\\nb/;\nconst c = 1;\nvar leak = 2;\n"
	if err := os.WriteFile(filepath.Join(dir, "nl.js"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LintNoVarJS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasErrors() || !strings.Contains(res.Error(), "nl.js:4:") {
		t.Fatalf("var on line 4 must be reported at line 4, got: %v", res)
	}
}
