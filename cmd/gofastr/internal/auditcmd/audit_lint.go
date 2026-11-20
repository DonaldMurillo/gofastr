package auditcmd

import (
	"fmt"
	"go/parser"
	goscanner "go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

// LintFinding is one violation reported by `gofastr audit lint`.
type LintFinding struct {
	File    string
	Line    int
	Rule    string
	Message string
	Snippet string
}

// auditLint scans root and returns one finding per detected violation.
// Tests can call this directly without going through runAudit.
func auditLint(root string) ([]LintFinding, error) {
	var all []LintFinding
	err := sourcewalk.Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Generated files (the conventional `// Code generated … DO NOT
		// EDIT.` header) get skipped. The developer can't fix findings
		// there, only the generator can. The FULL header is required, not
		// either half: contracts.IsGeneratedSource is the shared predicate
		// (it replaced this file's local either-half copy).
		if contracts.IsGeneratedSource(body) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "" {
			rel = path
		}
		all = append(all, lintFile(rel, body)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// App spec rule: unscoped PII (AGENTS.md hard rule #6) in a
	// conventional gofastr.yml/.yaml/.json at the audited root.
	all = append(all, lintAppSpecPIIRoot(root)...)
	sort.Slice(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		if all[i].Line != all[j].Line {
			return all[i].Line < all[j].Line
		}
		return all[i].Rule < all[j].Rule
	})
	return all, nil
}

func lintFile(rel string, body []byte) []LintFinding {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, parser.ParseComments)
	if err != nil {
		line := 1
		if failures, ok := err.(goscanner.ErrorList); ok && len(failures) > 0 {
			line = failures[0].Pos.Line
		}
		return []LintFinding{{File: rel, Line: line, Rule: "parse-error", Message: "Go source could not be checked: " + err.Error()}}
	}
	var out []LintFinding
	for _, d := range analyzers.AuditLint(rel, body, fset, file) {
		out = append(out, LintFinding{
			File: d.File, Line: d.Line, Rule: auditLintRules[d.RuleID], Message: d.Message, Snippet: d.Snippet,
		})
	}
	if !strings.HasSuffix(rel, "_test.go") {
		out = append(out, lintGoSourcePII(rel, fset, file)...)
	}
	return out
}

var auditLintRules = map[string]string{
	contracts.RuleSQLStringConcat: "sql-concat-user-input",
	contracts.RuleFormWithoutCSRF: "form-without-csrf",
	contracts.RuleHTMLConcat:      "render-html-concat",
	contracts.RuleIgnoredExec:     "ignored-exec",
	contracts.RuleDisabledTest:    "test-skip",
}

func formatLintReport(findings []LintFinding) string {
	if len(findings) == 0 {
		return "  ✓ No findings.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  Found %d issue(s):\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s:%d  [%s]  %s\n", f.File, f.Line, f.Rule, f.Message)
		if f.Snippet != "" {
			fmt.Fprintf(&b, "      %s\n", f.Snippet)
		}
	}
	return b.String()
}
