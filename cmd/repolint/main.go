package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type finding struct {
	File    string
	Line    int
	Rule    string
	Message string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	findings, err := lintRepo(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "repolint: %v\n", err)
		os.Exit(1)
	}
	if len(findings) == 0 {
		fmt.Println("  ok repo lint clean")
		return
	}
	fmt.Fprintf(os.Stderr, "  found %d repo lint issue(s):\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "  %s:%d [%s] %s\n", f.File, f.Line, f.Rule, f.Message)
	}
	os.Exit(1)
}

func lintRepo(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name(), path == root) {
				return fs.SkipDir
			}
			return nil
		}
		if name := d.Name(); hasControlChar(name) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			// Don't ToSlash — a newline in the name would render the path
			// unreadable; quote it so the finding is legible.
			findings = append(findings, finding{
				File:    strconv.Quote(filepath.ToSlash(rel)),
				Line:    1,
				Rule:    "bad-filename",
				Message: "file name contains a control character (likely a botched edit artifact)",
			})
		}
		if !isLintedFile(path) {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		findings = append(findings, lintBytes(rel, body)...)
		if strings.HasSuffix(path, ".go") {
			findings = append(findings, lintGoSyntax(rel, path, body)...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	truthFindings, err := lintRepositoryTruth(root)
	if err != nil {
		return nil, err
	}
	findings = append(findings, truthFindings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings, nil
}

func shouldSkipDir(name string, isRoot bool) bool {
	if isRoot {
		return false
	}
	switch name {
	case ".git", "vendor", "node_modules", "dist", "bin", "build", "tmp":
		return true
	}
	return strings.HasPrefix(name, ".")
}

func isLintedFile(path string) bool {
	name := filepath.Base(path)
	switch name {
	case "Makefile", "Dockerfile", "go.mod", "go.sum":
		return true
	}
	switch filepath.Ext(path) {
	case ".go", ".md", ".sh", ".yml", ".yaml", ".json", ".css", ".js", ".ts", ".tsx", ".html":
		return true
	default:
		return false
	}
}

func lintBytes(rel string, body []byte) []finding {
	var out []finding
	if bytes.Contains(body, []byte("\r\n")) {
		out = append(out, finding{
			File:    rel,
			Line:    1,
			Rule:    "crlf",
			Message: "file uses CRLF line endings",
		})
	}
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if isConflictMarker(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "conflict-marker",
				Message: "merge conflict marker left in source",
			})
		}
		if isBuildScript(rel) && mentionsExternalLintTool(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "external-lint-tool",
				Message: "build linting must use repo-owned checks or Go-team tools only",
			})
		}
		if rel == "go.mod" && mentionsExternalLintDependency(line) {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "external-lint-dependency",
				Message: "lint dependencies must stay repo-owned or Go-team tools only",
			})
		}
		if isBuildScript(rel) && strings.Contains(line, "framework/apiversions") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "retired-package-path",
				Message: "framework/apiversions moved to framework/experimental/apiversions",
			})
		}
		if isBuildScript(rel) && strings.Contains(line, ".pi/worktrees/roadmap") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "worktree-specific-script",
				Message: "build scripts must resolve from the repository root, not a personal worktree path",
			})
		}
		if rel == "Makefile" && strings.Contains(line, "No codegen yet") {
			out = append(out, finding{
				File:    rel,
				Line:    i + 1,
				Rule:    "stale-codegen-status",
				Message: "GoFastr ships blueprint code generation; keep the generate target truthful",
			})
		}
	}
	return out
}

func lintRepositoryTruth(root string) ([]finding, error) {
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	minor := latestReleaseMinor(string(changelog))
	if minor == "" {
		return nil, nil
	}
	security, err := os.ReadFile(filepath.Join(root, "SECURITY.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	expected := "currently `" + minor + ".x`"
	if strings.Contains(string(security), expected) {
		return nil, nil
	}
	return []finding{{
		File:    "SECURITY.md",
		Line:    lineContaining(string(security), "currently `"),
		Rule:    "supported-version-drift",
		Message: "supported release must match latest CHANGELOG release (want " + minor + ".x)",
	}}, nil
}

func latestReleaseMinor(changelog string) string {
	for _, line := range strings.Split(changelog, "\n") {
		if !strings.HasPrefix(line, "## [") || strings.HasPrefix(line, "## [Unreleased]") {
			continue
		}
		end := strings.Index(line, "]")
		if end < len("## [") {
			continue
		}
		parts := strings.Split(line[len("## ["):end], ".")
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
	}
	return ""
}

func lineContaining(body, needle string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 1
}

// hasControlChar reports whether s contains any ASCII control byte
// (including newline/tab/CR). Legitimate file names never do; a botched
// multi-line edit that lands a prompt fragment as a filename does.
func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func isConflictMarker(line string) bool {
	return strings.HasPrefix(line, "<<<<<<< ") ||
		strings.HasPrefix(line, "=======") ||
		strings.HasPrefix(line, ">>>>>>> ")
}

func isBuildScript(rel string) bool {
	if rel == "Makefile" {
		return true
	}
	return strings.HasPrefix(rel, "scripts/") && strings.HasSuffix(rel, ".sh")
}

func mentionsExternalLintTool(line string) bool {
	return strings.Contains(line, "golangci-lint") ||
		strings.Contains(line, "staticcheck")
}

func mentionsExternalLintDependency(line string) bool {
	for _, mod := range []string{
		"github.com/golangci/golangci-lint",
		"honnef.co/go/tools",
		"mvdan.cc/gofumpt",
	} {
		if strings.Contains(line, mod) {
			return true
		}
	}
	return false
}

func lintGoSyntax(rel, path string, body []byte) []finding {
	if isGeneratedGo(body) {
		return nil
	}
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, path, body, parser.SkipObjectResolution)
	if err == nil {
		return nil
	}
	line := 1
	if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
		line = list[0].Pos.Line
	}
	return []finding{{
		File:    rel,
		Line:    line,
		Rule:    "go-syntax",
		Message: err.Error(),
	}}
}

func isGeneratedGo(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("// Code generated")) ||
		bytes.Contains(head, []byte("DO NOT EDIT"))
}
