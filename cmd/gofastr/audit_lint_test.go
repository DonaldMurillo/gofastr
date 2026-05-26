package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lintRules(t *testing.T, body string) []LintFinding {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "input.go")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	return got
}

func lintRulesNamed(t *testing.T, name, body string) []LintFinding {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	return got
}

func TestLintIgnoredExecFlaggedWithoutAnnotation(t *testing.T) {
	got := lintRules(t, `package x
func f() { _, _ = db.ExecContext(ctx, "INSERT INTO t VALUES (1)") }`)
	mustHaveRule(t, got, "ignored-exec")
}

func TestLintIgnoredExecAllowedWithInlineAnnotation(t *testing.T) {
	got := lintRules(t, `package x
func f() { _, _ = db.ExecContext(ctx, "INSERT INTO t VALUES (1)") // best-effort: telemetry can fail
}`)
	mustNotHaveRule(t, got, "ignored-exec")
}

func TestLintIgnoredExecAllowedWithPriorLineAnnotation(t *testing.T) {
	got := lintRules(t, `package x
func f() {
    // best-effort: stats write, drop on failure
    _, _ = db.Exec("INSERT INTO stats VALUES (1)")
}`)
	mustNotHaveRule(t, got, "ignored-exec")
}

func TestLintFormPOSTWithoutCSRFFlagged(t *testing.T) {
	got := lintRules(t, `package x
const tmpl = ` + "`" + `<form method="POST" action="/save"><input name="x"></form>` + "`")
	mustHaveRule(t, got, "form-without-csrf")
}

func TestLintFormPOSTWithCSRFInFileAllowed(t *testing.T) {
	got := lintRules(t, `package x
func render() { _ = CSRFInputFromCtx(nil) }
const tmpl = ` + "`" + `<form method="POST" action="/save"></form>` + "`")
	mustNotHaveRule(t, got, "form-without-csrf")
}

// File-level grep of "CSRFInputFromCtx" is too coarse: a file with
// FIVE forms and one CSRF call only protects one form. The remaining
// four must be flagged. Compare counts, not presence.
func TestLintFormPOSTCountsNotFileGrep(t *testing.T) {
	got := lintRules(t, `package x
const tmpl = ` + "`" + `
<form method="POST" action="/a"></form>
<form method="POST" action="/b"></form>
` + "`" + `
func render() { _ = CSRFInputFromCtx(nil) }`)
	// Two forms but only one CSRF call → one finding expected.
	count := 0
	for _, f := range got {
		if f.Rule == "form-without-csrf" {
			count++
		}
	}
	if count == 0 {
		t.Fatal("expected at least one form-without-csrf finding; file-level grep silently passes")
	}
}

// A TODO comment that mentions CSRFInputFromCtx must NOT count as
// protection. strings.Contains can't distinguish prose from call sites.
func TestLintFormPOSTCSRFInCommentDoesNotCount(t *testing.T) {
	got := lintRules(t, `package x
// TODO: wire CSRFInputFromCtx into this form before shipping
const tmpl = ` + "`" + `<form method="POST" action="/save"></form>` + "`")
	mustHaveRule(t, got, "form-without-csrf")
}

func TestLintRenderHTMLConcatFlagged(t *testing.T) {
	got := lintRules(t, `package x
func render(u string) any { return render.HTML("<p>"+u+"</p>") }`)
	mustHaveRule(t, got, "render-html-concat")
}

func TestLintRenderHTMLConcatAnnotationAllowed(t *testing.T) {
	got := lintRules(t, `package x
func render(u string) any { return render.HTML("<p>"+u+"</p>") // safe-html: u is a known constant
}`)
	mustNotHaveRule(t, got, "render-html-concat")
}

func TestLintSQLConcatFlagged(t *testing.T) {
	got := lintRules(t, `package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='"+name+"'") }`)
	mustHaveRule(t, got, "sql-concat-user-input")
}

func TestLintTSkipFlaggedInTestFile(t *testing.T) {
	got := lintRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) { t.Skip("not yet") }`)
	mustHaveRule(t, got, "test-skip")
}

func TestLintTSkipAllowedWithAnnotation(t *testing.T) {
	got := lintRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) {
    // allow-skip: requires GPU, not available in CI
    t.Skip("no gpu")
}`)
	mustNotHaveRule(t, got, "test-skip")
}

// New: SQL-via-fmt.Sprintf with user input
func TestLintSQLSprintfFlagged(t *testing.T) {
	got := lintRules(t, `package x
import "fmt"
func q(name string) { db.Query(fmt.Sprintf("SELECT * FROM users WHERE name=%s", name)) }`)
	mustHaveRule(t, got, "sql-concat-user-input")
}

// New: query-builder .Where with `+` concat — common SQLi anti-pattern
// from agent-generated code. Should be flagged even without a SQL
// keyword in the literal.
func TestLintQueryBuilderWhereConcatFlagged(t *testing.T) {
	got := lintRules(t, `package x
func q(userID string) { qb.Where("user_id = " + userID) }`)
	mustHaveRule(t, got, "sql-concat-user-input")
}

// Generated files must be skipped entirely so generated SQL helpers
// don't flood the lint output.
func TestLintSkipsGeneratedFiles(t *testing.T) {
	got := lintRulesNamed(t, "gen.go", `// Code generated by foo. DO NOT EDIT.
package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='"+name+"'") }`)
	mustNotHaveRule(t, got, "sql-concat-user-input")
}

// Build / dist / bin / tmp directories must be skipped by the walker.
func TestLintSkipsDistDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "leak.go"), []byte(`package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='"+name+"'") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := auditLint(dir)
	if err != nil {
		t.Fatalf("auditLint: %v", err)
	}
	for _, f := range got {
		if strings.Contains(f.File, "dist/") {
			t.Fatalf("dist/ file was linted: %+v", f)
		}
	}
}

func TestLintTSkipNotFlaggedInProdFile(t *testing.T) {
	got := lintRulesNamed(t, "main.go", `package x
func f(t any) { _ = t }
// t.Skip("looks like one but isn't a test file")
`)
	mustNotHaveRule(t, got, "test-skip")
}

func mustHaveRule(t *testing.T, got []LintFinding, rule string) {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule {
			return
		}
	}
	t.Fatalf("rule %q not in findings: %+v", rule, got)
}

func mustNotHaveRule(t *testing.T, got []LintFinding, rule string) {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule {
			t.Fatalf("rule %q unexpectedly fired: %+v", rule, f)
		}
	}
}
