package evalrunner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIShimLogsAndExecsRealBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based shim exec test; the .cmd variant is exercised on Windows runs")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real-gofastr")
	if err := os.WriteFile(real, []byte("#!/bin/sh\necho REAL-RAN $1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "cli.log")
	shimDir := filepath.Join(dir, "shim")
	if err := installCLIShim(shimDir, real, logPath); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(filepath.Join(shimDir, "gofastr"), "docs", "ui-composition-recipes").CombinedOutput()
	if err != nil {
		t.Fatalf("shim exec: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "REAL-RAN docs") {
		t.Fatalf("shim did not exec the real binary: %q", out)
	}
	if _, err := exec.Command(filepath.Join(shimDir, "gofastr"), "dev", "-p", "1").CombinedOutput(); err != nil {
		t.Fatalf("second shim exec: %v", err)
	}

	calls, usedDev := cliInvocationStats(logPath)
	if calls != 2 || !usedDev {
		b, _ := os.ReadFile(logPath)
		t.Fatalf("stats = (%d, %t), want (2, true); log:\n%s", calls, usedDev, b)
	}
}

func TestCLIInvocationStatsMissingLogMeansZero(t *testing.T) {
	calls, usedDev := cliInvocationStats(filepath.Join(t.TempDir(), "never-written.log"))
	if calls != 0 || usedDev {
		t.Fatalf("missing log must read as no invocations, got (%d, %t)", calls, usedDev)
	}
}

func TestCLIInvocationStatsDistinguishesDevFromOtherSubcommands(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "cli.log")
	if err := os.WriteFile(logPath, []byte("docs ui-composition-recipes\ntheme init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls, usedDev := cliInvocationStats(logPath)
	if calls != 2 || usedDev {
		t.Fatalf("docs/theme-only log must not count as dev usage, got (%d, %t)", calls, usedDev)
	}
}

// The funnel signal must reach the human-readable output, not just JSON.
func TestLeaderboardReportsDevLoopFunnel(t *testing.T) {
	summary := Summary{Candidates: []CandidateResult{{
		VariantID: "working-tree", ScenarioID: "s", Repetition: 1,
		BuilderCLICalls: 3, BuilderUsedDevServer: true,
	}}}
	md := leaderboardMarkdown(summary)
	if !strings.Contains(md, "builder invoked `gofastr dev` `true`") || !strings.Contains(md, "3 gofastr CLI call(s)") {
		t.Fatalf("leaderboard missing dev-loop funnel line:\n%s", md)
	}
}
