package auditcmd

import (
	"strings"
	"testing"
)

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
		masked := stripGoComments([]byte(body))
		if len(masked) != len(body) {
			t.Fatalf("mask changed byte length: got %d want %d", len(masked), len(body))
		}
		for offset, character := range []byte(body) {
			if (character == '\n' || character == '\r') && masked[offset] != character {
				t.Fatalf("mask changed newline byte at %d", offset)
			}
		}
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
