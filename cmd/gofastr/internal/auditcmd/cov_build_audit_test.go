package auditcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── audit.go runAudit ─────────────────────────────────────────────────

func TestHelpOnUnknownSubcommandExits2(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"lint", "--help"}, 2},
		{[]string{"nope", "-h"}, 2},
		{[]string{"--help"}, 0},
		{[]string{"deps", "--help"}, 0},
	} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { RunAudit(tc.args) })
		})
		if code != tc.code {
			t.Errorf("audit %v: exit %d, want %d", tc.args, code, tc.code)
		}
	}
}

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
	mustWrite(t, dir, "main.go", "package main\n\nimport \"github.com/DonaldMurillo/gofastr/core-ui/style\"\n\nfunc init() { style.Contribute(nil) }\n\nfunc main() {}\n")
	mustWrite(t, dir, "incomplete.go", "package main\nfunc incomplete(")
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { RunAudit([]string{"deps", dir}) })
	})
	if code != -1 || !strings.HasPrefix(out, "Candidate global registration call sites\n") ||
		!strings.Contains(out, "audittest\n  style.Contribute ×1\n    main.go:5\n") {
		t.Fatalf("deps report: exit %d, output %q", code, out)
	}
	mustWrite(t, dir, "gofastr.settings.yml", "unmounted: invalid\n")
	code = clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunAudit([]string{"deps", dir}) })
	})
	if code != 1 {
		t.Errorf("invalid unmounted settings: exit %d, want 1", code)
	}
}
