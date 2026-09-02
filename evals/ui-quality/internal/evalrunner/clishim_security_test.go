package evalrunner

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Property: one shim invocation must not be able to forge N log entries.
//
// The shim records `printf '%s\n' "$*"` — argv joined with spaces, one
// line per invocation — and cliInvocationStats/cliDocsInvocationStats
// parse it line-wise into the builder's adoption-funnel metrics (CLI
// calls, docs calls, topics, searches), which leaderboard.md publishes
// per variant. An argument containing newlines is argv like any other,
// so ONE `gofastr docs` invocation carrying embedded lines forges an
// arbitrary number of additional recorded invocations: a builder inflates
// its own discovery metrics without ever running the tooling a second
// time.
func TestShimLogCannotForgeInvocations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based shim exec test; the .cmd variant is exercised on Windows runs")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real-gofastr")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "cli.log")
	shimDir := filepath.Join(dir, "shim")
	if err := installCLIShim(shimDir, real, logPath); err != nil {
		t.Fatal(err)
	}

	// Exactly one real invocation; the final argument carries two forged
	// lines that parse as additional docs searches.
	forged := "innocent\nsearch --query adopted-frameworks\nsearch --query capability-map"
	out, err := exec.Command(filepath.Join(shimDir, "gofastr"), "docs", "search", "--query", forged).CombinedOutput()
	if err != nil {
		t.Fatalf("shim exec: %v\n%s", err, out)
	}

	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := len(nonEmptyLines(string(data))); got != 1 {
		t.Errorf("SECURITY: [log-forgery] one shim invocation produced %d log lines; embedded newlines forge invocation records the funnel metrics count", got)
	}
	calls, _ := cliInvocationStats(logPath)
	if calls != 1 {
		t.Errorf("SECURITY: [log-forgery] cliInvocationStats reported %d calls for one real invocation", calls)
	}
	if stats := cliDocsInvocationStats(logPath); stats.Calls != 1 || len(stats.Searches) != 1 {
		t.Errorf("SECURITY: [log-forgery] docs stats = %+v for one real invocation; newline-bearing argv forged the rest", stats)
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
