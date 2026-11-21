package analyzers_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

type ruleFinding struct {
	File, Rule string
	Line       int
}

func verifyRules(t *testing.T, body string) []ruleFinding {
	t.Helper()
	return verifyRulesNamed(t, "input.go", body)
}

func verifyRulesNamed(t *testing.T, name, body string) []ruleFinding {
	t.Helper()
	dir := t.TempDir()
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return verifyDir(t, dir)
}

func verifyDir(t *testing.T, dir string) []ruleFinding {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	report, err := contracts.Run(pass, contracts.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []ruleFinding
	for _, d := range report.Diagnostics {
		out = append(out, ruleFinding{File: d.File, Rule: d.RuleID, Line: d.Line})
	}
	return out
}

func mustHaveRule(t *testing.T, got []ruleFinding, rule string) {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule {
			return
		}
	}
	t.Fatalf("rule %q not in findings: %+v", rule, got)
}

func mustNotHaveRule(t *testing.T, got []ruleFinding, rule string) {
	t.Helper()
	for _, f := range got {
		if f.Rule == rule {
			t.Fatalf("rule %q unexpectedly fired: %+v", rule, f)
		}
	}
}

func TestSQLWaiverAppliesOnlyToItsStatement(t *testing.T) {
	for _, tc := range []struct {
		name, statements string
		want             []int
	}{
		{"inline", "db.Query(\"SELECT \" + userInput) // safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)", []int{4}},
		{"preceding", "// safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)\ndb.Query(\"SELECT \" + userInput)", []int{5}},
		{"multiline", "db.Query(\"SELECT \" +\nuserInput) // safe-sql: checked column\ndb.Query(`SELECT ` + userInput)", []int{5}},
		{"multiline first line", "db.Query(\"SELECT \" + // safe-sql: checked column\nuserInput)\ndb.Query(\"SELECT \" + userInput)", []int{5}},
		{"multiline argument line", "db.Query(\n\"SELECT \" + userInput, // safe-sql: checked column\n1)\ndb.Query(\"SELECT \" + userInput)", []int{6}},
		{"if init trailing", "if _, err := db.Exec(\"SELECT \" + userInput); err != nil { // safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)\n}", []int{4}},
		{"if init preceding", "// safe-sql: checked column\nif _, err := db.Exec(\"SELECT \" + userInput); err != nil {\ndb.Query(\"SELECT \" + userInput)\n}", []int{5}},
		{"switch init", "switch _, err := db.Exec(\"SELECT \" + userInput); { // safe-sql: checked column\ncase err != nil: db.Query(\"SELECT \" + userInput)\n}", []int{4}},
		{"switch tag", "switch db.QueryRow(\"SELECT \" + userInput).Err() { // safe-sql: checked column\ndefault: db.Query(\"SELECT \" + userInput)\n}", []int{4}},
		{"range trailing", "for _, id := range ids(db.Query(\"SELECT \" + userInput)) { // safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)\n}", []int{4}},
		{"range preceding", "// safe-sql: checked column\nfor _, id := range ids(db.Query(\"SELECT \" + userInput)) {\ndb.Query(\"SELECT \" + userInput)\n}", []int{5}},
		{"if body trailing", "if ok { db.Query(\"SELECT \" + userInput) // safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)\n}", []int{4}},
		{"empty reason", "db.Query(\"SELECT \" + userInput) // safe-sql: \ndb.Query(\"SELECT \" + userInput)", []int{3, 4}},
		{"string", "db.Query(\"SELECT safe-sql: checked column \" + userInput)\ndb.Query(\"SELECT \" + userInput)", []int{3, 4}},
		{"same line request first", "db.Query(\"SELECT \" + userInput); db.Query(\"SELECT \" + checkedColumn) // safe-sql: checked column", []int{3}},
		{"same line request last", "db.Query(\"SELECT \" + checkedColumn); db.Query(\"SELECT \" + userInput) // safe-sql: checked column", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package app\nfunc query(userInput string) {\n" + tc.statements + "\n}\n"
			if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			verify, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security"}})
			if err != nil {
				t.Fatal(err)
			}
			var verifyLines []int
			for _, d := range verify.Diagnostics {
				if d.RuleID == contracts.RuleSQLStringConcat {
					verifyLines = append(verifyLines, d.Line)
				}
			}
			if !slices.Equal(verifyLines, tc.want) {
				t.Errorf("want lines %v, got %v", tc.want, verifyLines)
			}
		})
	}
}

func TestAuditRuleCases(t *testing.T) {
	for _, tc := range []struct {
		name, file, source, rule string
		want                     int
	}{
		{"raw SQL", "app.go", "package app\nfunc query(userInput string) { db.Query(`SELECT ` + userInput) }",
			contracts.RuleSQLStringConcat, 1},
		{"split SQL", "app.go", "package app\nfunc query(userInput string) { db.Query(\"SELECT \" +\n userInput) }",
			contracts.RuleSQLStringConcat, 1},
		{"CSRF inside literal comment", "app.go", "package app\nconst page = `<form method=\"POST\"></form> /* name=\"_csrf\" */`",
			contracts.RuleFormWithoutCSRF, 1},
		{"formatted HTML", "app.go", `package app
import "fmt"
import "github.com/DonaldMurillo/gofastr/core/render"
func view(input string) { render.HTML(fmt.Sprintf("<p>%s</p>", input)) }`,
			contracts.RuleHTMLConcat, 1},
		{"discarded Exec", "app.go", "package app\nfunc cleanup() { db.Exec(\"DELETE FROM sessions\") }",
			contracts.RuleIgnoredExec, 1},
		{"annotated Exec", "app.go", "package app\nfunc cleanup() { db.Exec(\"DELETE FROM sessions\") } // ignore the error: shutdown cleanup",
			contracts.RuleIgnoredExec, 0},
		{"lane phrase outside arguments", "app_test.go", `package app
import "testing"
func TestDebt(t *testing.T) {
	t.Skip("temporarily disabled") // Postgres unavailable in another lane
}
func TestLane(t *testing.T) {
	t.Skip(
		"Postgres unavailable")
}`,
			contracts.RuleDisabledTest, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			verify, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security", "data", "testing"}})
			if err != nil {
				t.Fatal(err)
			}
			var verifyLines []int
			for _, d := range verify.Diagnostics {
				if d.RuleID == tc.rule {
					verifyLines = append(verifyLines, d.Line)
				}
			}
			if len(verifyLines) != tc.want {
				t.Fatalf("want %d finding(s), got lines %v", tc.want, verifyLines)
			}
		})
	}
}

func TestAuditRuleIgnoredExecFlaggedWithoutAnnotation(t *testing.T) {
	got := verifyRules(t, `package x
func f() { _, _ = db.ExecContext(ctx, "INSERT INTO t VALUES (1)") }`)
	mustHaveRule(t, got, contracts.RuleIgnoredExec)
}

func TestAuditRuleIgnoredExecAllowedWithInlineAnnotation(t *testing.T) {
	got := verifyRules(t, `package x
func f() { _, _ = db.ExecContext(ctx, "INSERT INTO t VALUES (1)") // best-effort: telemetry can fail
}`)
	mustNotHaveRule(t, got, contracts.RuleIgnoredExec)
}

func TestAuditRuleIgnoredExecLiteralIsNotAnAnnotation(t *testing.T) {
	got := verifyRules(t, `package x
func f() { db.Exec("UPDATE jobs SET mode = 'best-effort'") }`)
	mustHaveRule(t, got, contracts.RuleIgnoredExec)
}

func TestAuditRuleIgnoredExecAllowedWithPriorLineAnnotation(t *testing.T) {
	got := verifyRules(t, `package x
func f() {
    // best-effort: stats write, drop on failure
    _, _ = db.Exec("INSERT INTO stats VALUES (1)")
}`)
	mustNotHaveRule(t, got, contracts.RuleIgnoredExec)
}

func TestAuditRuleFormPOSTWithoutCSRFFlagged(t *testing.T) {
	got := verifyRules(t, `package x
const tmpl = `+"`"+`<form method="POST" action="/save"><input name="x"></form>`+"`")
	mustHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

func TestAuditRuleFormPOSTWithCSRFInFileAllowed(t *testing.T) {
	got := verifyRules(t, `package x
func render() { _ = CSRFInputFromCtx(nil) }
const tmpl = `+"`"+`<form method="POST" action="/save"></form>`+"`")
	mustNotHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

func TestAuditRuleFormPOSTWithRawCSRFFieldAllowed(t *testing.T) {
	got := verifyRules(t, `package x
const tmpl = `+"`"+`<form method="POST"><input type="hidden" name="_csrf" value="token"></form>`+"`")
	mustNotHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

func TestAuditRuleFormPOSTWithDocumentedExemptionAllowed(t *testing.T) {
	got := verifyRules(t, `package x
// csrf-exempt: handler rejects cross-origin form posts before mutation.
const tmpl = `+"`"+`<form method="POST"></form>`+"`")
	mustNotHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

// File-level grep of "CSRFInputFromCtx" is too coarse: a file with
// FIVE forms and one CSRF call only protects one form. The remaining
// four must be flagged. Compare counts, not presence.
func TestAuditRuleFormPOSTCountsNotFileGrep(t *testing.T) {
	got := verifyRules(t, `package x
const tmpl = `+"`"+`
<form method="POST" action="/a"></form>
<form method="POST" action="/b"></form>
`+"`"+`
func render() { _ = CSRFInputFromCtx(nil) }`)
	// Two forms but only one CSRF call → one finding expected.
	count := 0
	for _, f := range got {
		if f.Rule == contracts.RuleFormWithoutCSRF {
			count++
		}
	}
	if count == 0 {
		t.Fatal("expected at least one form-without-csrf finding; file-level grep silently passes")
	}
}

// A TODO comment that mentions CSRFInputFromCtx must NOT count as
// protection. strings.Contains can't distinguish prose from call sites.
func TestAuditRuleFormPOSTCSRFInCommentDoesNotCount(t *testing.T) {
	got := verifyRules(t, `package x
// TODO: wire CSRFInputFromCtx into this form before shipping
const tmpl = `+"`"+`<form method="POST" action="/save"></form>`+"`")
	mustHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

func TestAuditRuleRenderHTMLConcatFlagged(t *testing.T) {
	got := verifyRules(t, `package x
func render(u string) any { return render.HTML("<p>"+u+"</p>") }`)
	mustHaveRule(t, got, contracts.RuleHTMLConcat)
}

func TestAuditRuleRenderHTMLConcatAnnotationAllowed(t *testing.T) {
	got := verifyRules(t, `package x
func render(u string) any { return render.HTML("<p>"+u+"</p>") // safe-html: u is a known constant
}`)
	mustNotHaveRule(t, got, contracts.RuleHTMLConcat)
}

func TestAuditRuleRenderHTMLConcatLiteralIsNotAnAnnotation(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"concatenated", `package x
func view(userInput string) any { return render.HTML("<p>safe-html: " + userInput + "</p>") }`},
		{"formatted", `package x
import "fmt"
import "github.com/DonaldMurillo/gofastr/core/render"
func view(userInput string) any {
	return render.HTML(fmt.Sprintf("<p>// safe-html: %s</p>", userInput))
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustHaveRule(t, verifyRules(t, tc.input), contracts.RuleHTMLConcat)
		})
	}
}

func TestAuditRuleRenderHTMLConcatAllowedWithPriorLineAnnotation(t *testing.T) {
	got := verifyRules(t, `package x
func view(u string) any {
	/* safe-html: u is a known constant */
	return render.HTML("<p>" + u + "</p>")
}`)
	mustNotHaveRule(t, got, contracts.RuleHTMLConcat)
}

func TestAuditRuleSQLConcatFlagged(t *testing.T) {
	got := verifyRules(t, `package x
func q(userInput string) { db.Query("SELECT * FROM users WHERE name='"+userInput+"'") }`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

func TestAuditRuleSQLConcatIgnoredInTestFixtures(t *testing.T) {
	got := verifyRulesNamed(t, "query_test.go", `package x
func q(userInput string) { db.Query("SELECT * FROM users WHERE name='"+userInput+"'") }`)
	mustNotHaveRule(t, got, contracts.RuleSQLStringConcat)
}

func TestAuditRuleSQLConcatDoesNotFlagURLOrDOMIdentifiers(t *testing.T) {
	got := verifyRules(t, `package x
func links(id string) {
	_ = "opt-delete-" + id
	_ = "/items/delete?id=" + id
}`)
	mustNotHaveRule(t, got, contracts.RuleSQLStringConcat)
}

func TestAuditRuleTSkipFlaggedInTestFile(t *testing.T) {
	got := verifyRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) { t.Skip("not yet") }`)
	mustHaveRule(t, got, contracts.RuleDisabledTest)
}

func TestAuditRuleTSkipAllowedWithAnnotation(t *testing.T) {
	got := verifyRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) {
    // allow-skip: requires GPU, not available in CI
    t.Skip("no gpu")
}`)
	mustNotHaveRule(t, got, contracts.RuleDisabledTest)
}

func TestAuditRuleTSkipAllowedInsideTestingShortGuard(t *testing.T) {
	got := verifyRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e: -short")
	}
}`)
	mustNotHaveRule(t, got, contracts.RuleDisabledTest)
}

func TestAuditRuleTSkipAllowedForUnavailableEnvironment(t *testing.T) {
	got := verifyRulesNamed(t, "smoke_test.go", `package x
import "testing"
func TestX(t *testing.T) { t.Skip("Postgres unavailable: connection refused") }`)
	mustNotHaveRule(t, got, contracts.RuleDisabledTest)
}

// New: SQL-via-fmt.Sprintf with user input
func TestAuditRuleSQLSprintfFlagged(t *testing.T) {
	got := verifyRules(t, `package x
import "fmt"
func q(userInput string) { db.Query(fmt.Sprintf("SELECT * FROM users WHERE name=%s", userInput)) }`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

// New: query-builder .Where with `+` concat, common SQLi anti-pattern
// from agent-generated code. Should be flagged even without a SQL
// keyword in the literal.
func TestAuditRuleQueryBuilderWhereConcatFlagged(t *testing.T) {
	got := verifyRules(t, `package x
func q(userInput string) { qb.Where("user_id = " + userInput) }`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

// Generated files must be skipped entirely so generated SQL helpers
// don't flood the lint output.
func TestAuditRuleSkipsGeneratedFiles(t *testing.T) {
	got := verifyRulesNamed(t, "gen.go", `// Code generated by foo. DO NOT EDIT.
package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='"+name+"'") }`)
	mustNotHaveRule(t, got, contracts.RuleSQLStringConcat)
}

// Build / dist / bin / tmp directories must be skipped by the walker.
func TestAuditRuleSkipsDistDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "leak.go"), []byte(`package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='"+name+"'") }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := verifyDir(t, dir)
	for _, f := range got {
		if strings.Contains(f.File, "dist/") {
			t.Fatalf("dist/ file was linted: %+v", f)
		}
	}
}

func TestAuditRuleTSkipNotFlaggedInProdFile(t *testing.T) {
	got := verifyRulesNamed(t, "main.go", `package x
func f(t any) { _ = t }
// t.Skip("looks like one but isn't a test file")
`)
	mustNotHaveRule(t, got, contracts.RuleDisabledTest)
}

// SQLite/MySQL INSERT variants (OR IGNORE / OR REPLACE / IGNORE) must
// stay inside the SQL-keyword anchor.
func TestAuditRuleSQLSprintfInsertOrIgnoreFlagged(t *testing.T) {
	got := verifyRules(t, `package x
func q(userInput string) {
	_ = fmt.Sprintf("INSERT OR IGNORE INTO notes (body) VALUES (%s)", userInput)
}`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

// Quote-adjacent interpolation is intrinsically a value position,
// identifiers are never quoted, so it flags regardless of the
// variable's name.
func TestAuditRuleSQLQuotedConcatFlagsOrdinaryNames(t *testing.T) {
	got := verifyRules(t, `package x
func q(name string) { db.Query("SELECT * FROM users WHERE name='" + name + "'") }`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

func TestAuditRuleSQLQuotedSprintfFlagsOrdinaryNames(t *testing.T) {
	got := verifyRules(t, `package x
func q(id string) { _ = fmt.Sprintf("DELETE FROM sessions WHERE id='%s'", id) }`)
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

// A raw _csrf mention inside a comment must not count as protection.
func TestAuditRuleFormPOSTRawCSRFInCommentDoesNotCount(t *testing.T) {
	got := verifyRules(t, `package x
// TODO: add name="_csrf" once wired.
const tmpl = `+"`"+`<form method="POST"><button>Save</button></form>`+"`")
	mustHaveRule(t, got, contracts.RuleFormWithoutCSRF)
}

func TestAuditSQLAnnotationSurvivesCommentFiltering(t *testing.T) {
	got := verifyRules(t, "package app\nfunc query(userInput string) { db.Query(\"SELECT \" + userInput) } // safe-sql: allowlisted column identifier\n")
	mustNotHaveRule(t, got, contracts.RuleSQLStringConcat)
	got = verifyRules(t, "package app\nfunc query(userInput string) { db.Query(\"SELECT \" + userInput) }\n")
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
}

func TestAuditBlockCommentKeepsSQLLine(t *testing.T) {
	got := verifyRules(t, "package app\n/*\none\ntwo\n*/\nfunc query(userInput string) {\n db.Query(\"SELECT \" + userInput)\n}\n")
	mustHaveRule(t, got, contracts.RuleSQLStringConcat)
	for _, finding := range got {
		if finding.Rule == contracts.RuleSQLStringConcat && finding.Line != 7 {
			t.Fatalf("SQL location shifted: %+v", finding)
		}
	}
}

func TestAuditCRLFCommentsDoNotLeakSQL(t *testing.T) {
	for _, comment := range []string{
		"/*\r\n" + strings.Repeat("license\r\n", 80) + "db.Query(\"SELECT \" + userInput)\r\n*/\r\n",
		"// " + strings.Repeat("license\r", 80) + "db.Query(\"SELECT \" + userInput)\r\n",
	} {
		body := comment + "package app\r\n"
		mustNotHaveRule(t, verifyRules(t, body), contracts.RuleSQLStringConcat)
		live := body + "func query(userInput string) { db.Query(\"SELECT \" + userInput) }\r\n"
		findings := verifyRules(t, live)
		mustHaveRule(t, findings, contracts.RuleSQLStringConcat)
		for _, finding := range findings {
			if finding.Rule == contracts.RuleSQLStringConcat && finding.Line != strings.Count(body, "\n")+1 {
				t.Errorf("comment leaked or source location shifted: %+v", finding)
			}
		}
	}
}

func TestAuditBareExecResults(t *testing.T) {
	for _, prefix := range []string{"", "defer ", "go "} {
		t.Run(prefix, func(t *testing.T) {
			got := verifyRules(t, "package app\nfunc cleanup() { "+prefix+"db.Exec(\"DELETE FROM sessions\") }\n")
			mustHaveRule(t, got, contracts.RuleIgnoredExec)
			got = verifyRules(t, "package app\nfunc cleanup() { "+prefix+"db.Exec(\"DELETE FROM sessions\") } // best-effort: cleanup after failure\n")
			mustNotHaveRule(t, got, contracts.RuleIgnoredExec)
		})
	}
}

func TestAuditRuleFlagsSkipAndSkipf(t *testing.T) {
	for _, call := range []string{`t.Skip("flaky on arm64")`, `t.Skipf("flaky: %s", "arm64")`} {
		got := verifyRulesNamed(t, "skip_test.go", "package app\nimport \"testing\"\nfunc TestSkip(t *testing.T) { "+call+" }\n")
		mustHaveRule(t, got, contracts.RuleDisabledTest)
	}
}
