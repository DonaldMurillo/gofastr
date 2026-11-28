package gates

import (
	"fmt"
	"io"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

// BuildSandboxGateTo runs the .ui.go sandbox lint over root, writes its
// report to stdout and its error to stderr, and reports whether it passed.
func BuildSandboxGateTo(root string, stdout, stderr io.Writer) bool {
	return lintGate(root, stdout, stderr, ".ui.go sandbox lint", auditSandbox, formatSandboxReport)
}

// SandboxFinding is one .ui.go sandbox violation reported by the build gate.
type SandboxFinding struct {
	File    string
	Line    int
	Message string
}

func auditSandbox(root string) ([]SandboxFinding, error) {
	violations, err := auditTree(root, ".ui.go", check.LintFile)
	if err != nil {
		return nil, err
	}
	all := make([]SandboxFinding, 0, len(violations))
	for _, v := range violations {
		all = append(all, SandboxFinding(v))
	}
	return all, nil
}

func formatSandboxReport(findings []SandboxFinding) string {
	if len(findings) == 0 {
		return "No .ui.go sandbox issues found.\n"
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[f.File] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, ".ui.go sandbox lint: %d issue(s) in %d file(s)\n\n", len(findings), len(files))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s:%d: %s\n\n", f.File, f.Line, f.Message)
	}
	b.WriteString("These files hydrate client-side and may not use goroutines,\n")
	b.WriteString("channels, or imports outside the safe allow-list. Move the logic\n")
	b.WriteString("into a server-side handler or a non-.ui.go file.\n")
	return b.String()
}
