package auditcmd

import (
	"strings"
	"testing"
)

// ── audit_lint.go formatLintReport ────────────────────────────────────

func TestFormatLintReport(t *testing.T) {
	if !strings.Contains(formatLintReport(nil), "No findings") {
		t.Fatal("empty report")
	}
	out := formatLintReport([]LintFinding{
		{File: "a.go", Line: 3, Rule: "R", Message: "boom", Snippet: "x := 1"},
		{File: "b.go", Line: 9, Rule: "R2", Message: "bad"},
	})
	for _, want := range []string{"2 issue", "a.go:3", "[R]", "boom", "x := 1", "b.go:9"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}
