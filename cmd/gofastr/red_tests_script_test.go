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
	env := []string{"GOWORK=off", "GOFLAGS="}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFASTR_TEST_ALL=") || strings.HasPrefix(kv, "RED_OUT=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	got := string(out)
	if strings.Contains(got, "nothing to run") {
		t.Fatalf("red-tests.sh skipped the suite when cmd/affected failed:\n%s", got)
	}
	if !strings.Contains(got, "TestRedProbeOpen") {
		t.Fatalf("red-tests.sh did not run the open probe:\n%s", got)
	}
}
