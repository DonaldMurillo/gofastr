package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip the usual non-source trees + every build-output
			// directory the project uses. Generated code lives under
			// dist/bin/build/tmp and its findings drown out the real
			// signal from app code.
			switch name {
			case "vendor", ".git", "node_modules", "dist", "bin", "build", "tmp":
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Generated files (DO NOT EDIT header) get skipped — the
		// developer can't fix findings there, only the generator can.
		if isGeneratedFile(body) {
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

// isGeneratedFile reports whether body looks like a Go-generated file.
// Convention: first ~256 bytes contain `// Code generated` and/or
// `DO NOT EDIT.` per https://pkg.go.dev/cmd/go#hdr-Generate_Go_files.
func isGeneratedFile(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("// Code generated")) ||
		bytes.Contains(head, []byte("DO NOT EDIT"))
}

func lintFile(rel string, body []byte) []LintFinding {
	var out []LintFinding
	out = append(out, ruleIgnoredExec(rel, body)...)
	out = append(out, ruleFormWithoutCSRF(rel, body)...)
	out = append(out, ruleRenderHTMLConcat(rel, body)...)
	out = append(out, ruleSQLConcatUserInput(rel, body)...)
	out = append(out, ruleTestSkip(rel, body)...)
	return out
}

// ----------------------------------------------------------------------------
// Rule 1 — ignored db.Exec / tx.Exec result without best-effort annotation.
// ----------------------------------------------------------------------------

var reIgnoredExec = regexp.MustCompile(`(?:^|[\s;{])_,\s*_\s*=\s*\S+\.Exec(?:Context)?\b`)

func ruleIgnoredExec(rel string, body []byte) []LintFinding {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 1<<20), 1<<22)
	var out []LintFinding
	lineNum := 0
	prev := ""
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if !reIgnoredExec.MatchString(line) {
			prev = line
			continue
		}
		// Annotation may sit on the same line as an inline comment, or
		// on the immediately preceding non-blank line.
		annotated := strings.Contains(line, "best-effort:") ||
			strings.Contains(prev, "best-effort:")
		if !annotated {
			out = append(out, LintFinding{
				File:    rel,
				Line:    lineNum,
				Rule:    "ignored-exec",
				Message: "ignored Exec result without `// best-effort: …` annotation — wrap or fail loud",
				Snippet: strings.TrimSpace(line),
			})
		}
		prev = line
	}
	return out
}

// ----------------------------------------------------------------------------
// Rule 2 — <form method="POST"> in source without enough CSRFInputFromCtx
// call sites to cover every form. Counts forms vs call sites instead of
// doing a file-level grep so a single CSRF wiring doesn't appear to
// protect five different forms in the same file. Also ignores
// CSRFInputFromCtx mentions inside comments — strings.Contains can't
// distinguish prose from code.
// ----------------------------------------------------------------------------

var (
	reFormPOST    = regexp.MustCompile(`(?i)<form\b[^>]*method=["']?POST["']?`)
	reCSRFCallNon = regexp.MustCompile(`\bCSRFInputFromCtx\s*\(`)
)

func ruleFormWithoutCSRF(rel string, body []byte) []LintFinding {
	if strings.HasSuffix(rel, "_test.go") {
		return nil
	}
	// Strip line + block comments before counting CSRF call sites so a
	// "// TODO: wire CSRFInputFromCtx" doesn't count as protection.
	stripped := stripGoComments(body)
	csrfCalls := len(reCSRFCallNon.FindAllIndex(stripped, -1))

	var formLines []int
	for i, line := range strings.Split(string(body), "\n") {
		if reFormPOST.MatchString(line) {
			formLines = append(formLines, i)
		}
	}
	if len(formLines) <= csrfCalls {
		return nil
	}
	// Report the surplus forms — first N where N = forms − csrfCalls.
	// Don't try to guess which specific form lacks coverage; that
	// requires real parsing. Flagging the file accurately is enough.
	deficit := len(formLines) - csrfCalls
	var out []LintFinding
	lines := strings.Split(string(body), "\n")
	for i := 0; i < deficit; i++ {
		ln := formLines[i]
		out = append(out, LintFinding{
			File:    rel,
			Line:    ln + 1,
			Rule:    "form-without-csrf",
			Message: fmt.Sprintf("<form method=\"POST\"> count (%d) exceeds CSRFInputFromCtx call count (%d) — every POST form needs a CSRF input", len(formLines), csrfCalls),
			Snippet: strings.TrimSpace(lines[ln]),
		})
	}
	return out
}

// stripGoComments removes // line comments and /* … */ block comments
// so CSRF / lint annotations inside prose don't fool downstream regexes.
// Doesn't honor string literals — close enough for the kinds of files
// the lint pass cares about.
var (
	reLineComment  = regexp.MustCompile(`//[^\n]*`)
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

func stripGoComments(body []byte) []byte {
	b := reBlockComment.ReplaceAll(body, []byte(""))
	return reLineComment.ReplaceAll(b, []byte(""))
}

// ----------------------------------------------------------------------------
// Rule 3 — render.HTML(...) with `+` concat (likely interpolating user input).
// ----------------------------------------------------------------------------

var reRenderHTMLConcat = regexp.MustCompile(`render\.HTML\([^)]*\+[^)]*\)`)

func ruleRenderHTMLConcat(rel string, body []byte) []LintFinding {
	var out []LintFinding
	for i, line := range strings.Split(string(body), "\n") {
		if reRenderHTMLConcat.MatchString(line) && !strings.Contains(line, "// safe-html:") {
			out = append(out, LintFinding{
				File:    rel,
				Line:    i + 1,
				Rule:    "render-html-concat",
				Message: "render.HTML with `+` concat raises XSS risk — use render.Text for untrusted strings, or annotate `// safe-html: <why>`",
				Snippet: strings.TrimSpace(line),
			})
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// Rule 4 — SQL string-concat with user input. Three sub-patterns, all
// heuristic; every match is suppressible with `// safe-sql:` on the line.
//   (a) literal SQL keyword + `+ ident`
//       e.g. db.Query("SELECT * WHERE name='" + name + "'")
//   (b) fmt.Sprintf wrapping a SQL keyword with a `%s`/`%v` directive
//       e.g. fmt.Sprintf("SELECT * WHERE name=%s", userInput)
//   (c) query-builder methods (.Where / .Having / .OrderBy) called
//       with a `+`-concat argument — flagged regardless of keyword
//       in the literal, because qb.Where("user_id = " + id) is the
//       canonical SQLi anti-pattern from AI-generated code.
// ----------------------------------------------------------------------------

var (
	reSQLConcatLiteral = regexp.MustCompile(`(?i)"[^"]*\b(?:SELECT|INSERT|UPDATE|DELETE)\b[^"]*"\s*\+\s*\w+`)
	reSQLSprintf       = regexp.MustCompile(`(?i)fmt\.S?(?:print|printf)\(\s*"[^"]*\b(?:SELECT|INSERT|UPDATE|DELETE|WHERE|HAVING)\b[^"]*%[sv]`)
	reSQLBuilderConcat = regexp.MustCompile(`\.(?:Where|Having|OrderBy|GroupBy)\(\s*"[^"]*"\s*\+\s*\w+`)
)

func ruleSQLConcatUserInput(rel string, body []byte) []LintFinding {
	var out []LintFinding
	for i, line := range strings.Split(string(body), "\n") {
		if strings.Contains(line, "// safe-sql:") {
			continue
		}
		hit := reSQLConcatLiteral.MatchString(line) ||
			reSQLSprintf.MatchString(line) ||
			reSQLBuilderConcat.MatchString(line)
		if !hit {
			continue
		}
		out = append(out, LintFinding{
			File:    rel,
			Line:    i + 1,
			Rule:    "sql-concat-user-input",
			Message: "string-concat into a SQL statement looks like user-input interpolation — use $N / ? placeholders, or annotate `// safe-sql: <why>`",
			Snippet: strings.TrimSpace(line),
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// Rule 5 — t.Skip in test files without allow-skip annotation.
// ----------------------------------------------------------------------------

var reTSkip = regexp.MustCompile(`\bt\.Skip(?:f|Now)?\s*\(`)

func ruleTestSkip(rel string, body []byte) []LintFinding {
	if !strings.HasSuffix(rel, "_test.go") {
		return nil
	}
	var out []LintFinding
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if !reTSkip.MatchString(line) {
			continue
		}
		if strings.Contains(line, "// allow-skip:") {
			continue
		}
		// Allow-skip annotation may sit on the previous non-blank line.
		prev := ""
		for j := i - 1; j >= 0; j-- {
			if strings.TrimSpace(lines[j]) != "" {
				prev = lines[j]
				break
			}
		}
		if strings.Contains(prev, "// allow-skip:") {
			continue
		}
		out = append(out, LintFinding{
			File:    rel,
			Line:    i + 1,
			Rule:    "test-skip",
			Message: "t.Skip in tests hides missing coverage — hard-fail instead, or annotate `// allow-skip: <why>`",
			Snippet: strings.TrimSpace(line),
		})
	}
	return out
}

// ----------------------------------------------------------------------------
// Reporting.
// ----------------------------------------------------------------------------

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
