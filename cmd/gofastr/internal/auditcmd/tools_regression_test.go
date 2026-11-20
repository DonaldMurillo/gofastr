package auditcmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestSQLWaiverAppliesOnlyToItsStatement(t *testing.T) {
	for _, tc := range []struct {
		name, statements string
		want             []int
	}{
		{"inline", "db.Query(\"SELECT \" + userInput) // safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)", []int{4}},
		{"preceding", "// safe-sql: checked column\ndb.Query(\"SELECT \" + userInput)\ndb.Query(\"SELECT \" + userInput)", []int{5}},
		{"multiline", "db.Query(\"SELECT \" +\nuserInput) // safe-sql: checked column\ndb.Query(`SELECT ` + userInput)", []int{5}},
		{"empty reason", "db.Query(\"SELECT \" + userInput) // safe-sql: \ndb.Query(\"SELECT \" + userInput)", []int{3, 4}},
		{"string", "db.Query(\"SELECT safe-sql: checked column \" + userInput)\ndb.Query(\"SELECT \" + userInput)", []int{3, 4}},
		{"same line", "db.Query(\"SELECT \" + userInput); db.Query(\"SELECT \" + userInput) // safe-sql: checked column", []int{3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package app\nfunc query(userInput string) {\n" + tc.statements + "\n}\n"
			if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			audit, err := auditLint(dir)
			if err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			verify, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security"}})
			if err != nil {
				t.Fatal(err)
			}
			var auditLines, verifyLines []int
			for _, f := range audit {
				if f.Rule == "sql-concat-user-input" {
					auditLines = append(auditLines, f.Line)
				}
			}
			for _, d := range verify.Diagnostics {
				if d.RuleID == contracts.RuleSQLStringConcat {
					verifyLines = append(verifyLines, d.Line)
				}
			}
			if !slices.Equal(auditLines, tc.want) || !slices.Equal(verifyLines, tc.want) {
				t.Errorf("want lines %v, got audit %v and verify %v", tc.want, auditLines, verifyLines)
			}
		})
	}
}

func TestAuditLintTracksContractDetectors(t *testing.T) {
	for _, tc := range []struct {
		name, file, source, slug, rule string
		want                           int
	}{
		{"raw SQL", "app.go", "package app\nfunc query(userInput string) { db.Query(`SELECT ` + userInput) }",
			"sql-concat-user-input", contracts.RuleSQLStringConcat, 1},
		{"split SQL", "app.go", "package app\nfunc query(userInput string) { db.Query(\"SELECT \" +\n userInput) }",
			"sql-concat-user-input", contracts.RuleSQLStringConcat, 1},
		{"CSRF inside literal comment", "app.go", "package app\nconst page = `<form method=\"POST\"></form> /* name=\"_csrf\" */`",
			"form-without-csrf", contracts.RuleFormWithoutCSRF, 1},
		{"formatted HTML", "app.go", `package app
import "fmt"
import "github.com/DonaldMurillo/gofastr/core/render"
func view(input string) { render.HTML(fmt.Sprintf("<p>%s</p>", input)) }`,
			"render-html-concat", contracts.RuleHTMLConcat, 1},
		{"discarded Exec", "app.go", "package app\nfunc cleanup() { db.Exec(\"DELETE FROM sessions\") }",
			"ignored-exec", contracts.RuleIgnoredExec, 1},
		{"annotated Exec", "app.go", "package app\nfunc cleanup() { db.Exec(\"DELETE FROM sessions\") } // ignore the error: shutdown cleanup",
			"ignored-exec", contracts.RuleIgnoredExec, 0},
		{"lane phrase outside arguments", "app_test.go", `package app
import "testing"
func TestDebt(t *testing.T) {
	t.Skip("temporarily disabled") // Postgres unavailable in another lane
}
func TestLane(t *testing.T) {
	t.Skip(
		"Postgres unavailable")
}`,
			"test-skip", contracts.RuleDisabledTest, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.source), 0o600); err != nil {
				t.Fatal(err)
			}
			audit, err := auditLint(dir)
			if err != nil {
				t.Fatal(err)
			}
			pass, err := contracts.NewPass(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			verify, err := contracts.Run(pass, contracts.RunOptions{Analyzers: []string{"security", "data", "testing"}})
			if err != nil {
				t.Fatal(err)
			}
			var auditLines, verifyLines []int
			for _, f := range audit {
				if f.Rule == tc.slug {
					auditLines = append(auditLines, f.Line)
				}
			}
			for _, d := range verify.Diagnostics {
				if d.RuleID == tc.rule {
					verifyLines = append(verifyLines, d.Line)
				}
			}
			if len(auditLines) != tc.want || len(verifyLines) != tc.want {
				t.Fatalf("want %d finding(s) in both commands, got audit lines %v and verify lines %v", tc.want, auditLines, verifyLines)
			}
			if tc.want == 1 && auditLines[0] != verifyLines[0] {
				t.Fatalf("audit reported line %d, verify reported line %d", auditLines[0], verifyLines[0])
			}
		})
	}
}

func TestAuditSQLAnnotationSurvivesCommentFiltering(t *testing.T) {
	got := lintRules(t, "package app\nfunc query(userInput string) { db.Query(\"SELECT \" + userInput) } // safe-sql: allowlisted column identifier\n")
	mustNotHaveRule(t, got, "sql-concat-user-input")
	got = lintRules(t, "package app\nfunc query(userInput string) { db.Query(\"SELECT \" + userInput) }\n")
	mustHaveRule(t, got, "sql-concat-user-input")
}

func TestAuditMalformedTestIsNotClean(t *testing.T) {
	got := lintRulesNamed(t, "broken_test.go", "package app\nfunc TestBroken( {\n")
	mustHaveRule(t, got, "parse-error")
}

func TestAuditBlockCommentKeepsSQLLine(t *testing.T) {
	got := lintRules(t, "package app\n/*\none\ntwo\n*/\nfunc query(userInput string) {\n db.Query(\"SELECT \" + userInput)\n}\n")
	mustHaveRule(t, got, "sql-concat-user-input")
	for _, finding := range got {
		if finding.Rule == "sql-concat-user-input" && finding.Line != 7 {
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
		mustNotHaveRule(t, lintRules(t, body), "sql-concat-user-input")
		live := body + "func query(userInput string) { db.Query(\"SELECT \" + userInput) }\r\n"
		findings := lintRules(t, live)
		mustHaveRule(t, findings, "sql-concat-user-input")
		for _, finding := range findings {
			if finding.Rule == "sql-concat-user-input" && finding.Line != strings.Count(body, "\n")+1 {
				t.Errorf("comment leaked or source location shifted: %+v", finding)
			}
		}
	}
}

func TestAuditBareExecResults(t *testing.T) {
	for _, prefix := range []string{"", "defer ", "go "} {
		t.Run(prefix, func(t *testing.T) {
			got := lintRules(t, "package app\nfunc cleanup() { "+prefix+"db.Exec(\"DELETE FROM sessions\") }\n")
			mustHaveRule(t, got, "ignored-exec")
			got = lintRules(t, "package app\nfunc cleanup() { "+prefix+"db.Exec(\"DELETE FROM sessions\") } // best-effort: cleanup after failure\n")
			mustNotHaveRule(t, got, "ignored-exec")
		})
	}
}

func TestAuditSkipDoesNotRequireDebtVocabulary(t *testing.T) {
	for _, call := range []string{`t.Skip("flaky on arm64")`, `t.Skipf("flaky: %s", "arm64")`, `t.SkipNow()`} {
		got := lintRulesNamed(t, "skip_test.go", "package app\nimport \"testing\"\nfunc TestSkip(t *testing.T) { "+call+" }\n")
		mustHaveRule(t, got, "test-skip")
	}
}
