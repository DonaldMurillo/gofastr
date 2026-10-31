package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A baseline that is not one non-negative integer fails dupl.sh.
// `[ "$count" -gt "$baseline" ]` errors on a non-number, and inside
// an `if` that error is a false condition, so the gate used to fall
// through to its success line.
//
//gofastr:test-reads scripts/dupl.sh
func TestDuplScriptRefusesBadBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("runs bash and git in temp dirs")
	}
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " not on PATH")
		}
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "dupl.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, baseline := range []string{"abc\n", "\n", "-3\n", "1 2\n", "12abc\n"} {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(dir, "scripts", "dupl.sh"), string(src))
		writeFixture(t, filepath.Join(dir, "scripts", "dupl-baseline.txt"), baseline)
		// A tracked Go file, so a script that skipped the check would
		// scan and reach the comparison rather than stop on git.
		writeFixture(t, filepath.Join(dir, "a.go"), "package a\n")
		for _, args := range [][]string{{"init", "-q"}, {"add", "a.go"}} {
			git := exec.Command("git", args...)
			git.Dir = dir
			if out, err := git.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		cmd := exec.Command("bash", "scripts/dupl.sh")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("baseline %q: dupl.sh exited 0, want a refusal:\n%s", baseline, out)
			continue
		}
		if !strings.Contains(string(out), "not a clone-group count") {
			t.Errorf("baseline %q: output does not name the bad baseline:\n%s", baseline, out)
		}
	}
}
