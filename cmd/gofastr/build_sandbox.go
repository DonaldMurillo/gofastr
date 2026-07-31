package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

// buildSandboxGate runs the .ui.go hydration-sandbox lint for `gofastr build`
// and `gofastr dev`, and reports whether the build may proceed. The sandbox
// rules (check.LintFile: no goroutines, channels, type switches, or imports
// outside the safe allow-list) exist because client-hydrated .ui.go files
// cannot use them — a `go func(){}` or `import "os"` in a .ui.go file breaks
// hydration at runtime. Unlike the accessibility gate this is a correctness
// floor, so it is NOT bound to --no-a11y and runs on every build/dev rebuild.
func buildSandboxGate(root string) bool {
	findings, err := auditSandbox(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, ".ui.go sandbox lint: %v\n", err)
		return false
	}
	if len(findings) == 0 {
		return true
	}
	fmt.Print(formatSandboxReport(findings))
	return false
}

// SandboxFinding is one .ui.go sandbox violation reported by the build gate.
type SandboxFinding struct {
	File    string
	Line    int
	Message string
}

// auditSandbox statically scans every non-test, non-generated .ui.go file
// under root with check.LintFile — the goroutine/channel/import sandbox that
// LintA11yFile deliberately skips. Mirrors auditA11y's walk/skip policy so
// the two gates agree on which files are in scope.
func auditSandbox(root string) ([]SandboxFinding, error) {
	var all []SandboxFinding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case "vendor", ".git", "node_modules", "dist", "bin", "build", "tmp":
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		// The sandbox governs .ui.go files only; a plain .go file is free
		// to spawn goroutines. Generated .ui.go is the generator's job.
		if !strings.HasSuffix(path, ".ui.go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isGeneratedFile(body) {
			return nil
		}
		res, lintErr := check.LintFile(path)
		if lintErr != nil {
			// Mid-edit parse errors shouldn't kill the audit.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "" {
			rel = path
		}
		for _, v := range res.Violations {
			all = append(all, SandboxFinding{
				File:    rel,
				Line:    v.Line,
				Message: v.Message,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
	return all, nil
}

// formatSandboxReport renders .ui.go sandbox findings with a one-line summary.
func formatSandboxReport(findings []SandboxFinding) string {
	if len(findings) == 0 {
		return "No .ui.go sandbox issues found.\n"
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[f.File] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, ".ui.go sandbox lint — %d issue(s) in %d file(s)\n\n", len(findings), len(files))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s:%d: %s\n\n", f.File, f.Line, f.Message)
	}
	b.WriteString("These files hydrate client-side and may not use goroutines,\n")
	b.WriteString("channels, or imports outside the safe allow-list. Move the logic\n")
	b.WriteString("into a server-side handler or a non-.ui.go file.\n")
	return b.String()
}
