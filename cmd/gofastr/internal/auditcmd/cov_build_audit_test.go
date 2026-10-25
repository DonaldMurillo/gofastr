package auditcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── audit.go runAudit ─────────────────────────────────────────────────

func TestRunAuditUsageExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAudit(nil) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAuditUnknownExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAudit([]string{"frob"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunAuditDeps(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module audittest\n\ngo 1.21\n")
	mustWrite(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	out := clitest.CovT_capStdout(t, func() { RunAudit([]string{"deps", dir}) })
	_ = out // formatAuditReport always prints something
}

func TestRunAuditLintClean(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module linttest\n\ngo 1.21\n")
	mustWrite(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	out := clitest.CovT_capStdout(t, func() { RunAudit([]string{"lint", dir}) })
	if !strings.Contains(out, "No findings") {
		t.Fatalf("clean lint should report no findings: %s", out)
	}
}

func TestRunAuditLintFindingExits(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "go.mod", "module linttest\n\ngo 1.21\n")
	// A t.Skip in a test file triggers the ruleTestSkip finding.
	mustWrite(t, dir, "x_test.go", "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skip(\"todo\")\n}\n")
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAudit([]string{"lint", dir}) })
	})
	if code != 1 {
		t.Fatalf("findings should exit 1, got %d", code)
	}
}
