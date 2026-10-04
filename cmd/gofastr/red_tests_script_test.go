package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedScriptRunsAllWhenAffectedFails runs scripts/red-tests.sh in a
// temp module holding one open red probe and no cmd/affected, so the
// affected-package computation exits non-zero. The gate must degrade
// to every red-tagged package, never filter against an empty list and
// report "nothing to run".
func TestRedScriptRunsAllWhenAffectedFails(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test in a temp module")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "go.mod"), "module redprobe\n\ngo 1.22\n")
	if err := os.MkdirAll(filepath.Join(dir, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(dir, "probe", "probe_red_test.go"),
		"//go:build red\n\npackage probe\n\nimport \"testing\"\n\n"+
			"func TestRedProbeOpen(t *testing.T) { t.Fatal(\"finding still open\") }\n")
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "red-tests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(dir, "scripts", "red-tests.sh"), string(src))

	cmd := exec.Command("bash", "scripts/red-tests.sh")
	cmd.Dir = dir
	// The fixture's overrides go last: Go keeps the last value of a
	// duplicate key, so an inherited GOWORK pointing at a workspace that
	// excludes the temp module cannot win over GOWORK=off.
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFASTR_TEST_ALL=") || strings.HasPrefix(kv, "RED_OUT=") ||
			strings.HasPrefix(kv, "GOWORK=") || strings.HasPrefix(kv, "GOFLAGS=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "GOWORK=off", "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	got := string(out)
	// The script's contract: open findings are a report, not a failure.
	// It exits 0 after listing them and non-zero only when a red file
	// fails to compile or go test fails with no --- FAIL line, so the
	// exit status is asserted rather than ignored.
	if err != nil {
		t.Fatalf("red-tests.sh exited non-zero (%v) with one open probe:\n%s", err, got)
	}
	if strings.Contains(got, "nothing to run") {
		t.Fatalf("red-tests.sh skipped the suite when cmd/affected failed:\n%s", got)
	}
	if !strings.Contains(got, "1 open finding(s)") || !strings.Contains(got, "TestRedProbeOpen") {
		t.Fatalf("red-tests.sh did not report the open probe as a finding:\n%s", got)
	}
}
