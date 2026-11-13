package reviewgate

//gofastr:test-reads .claude/skills/review-gate/
//gofastr:test-reads /.githooks/pre-commit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// findDriver imports review3.py and prints what find_codex returns, or
// the not-found message. RG_PLATFORM and RG_OSNAME stand in for another
// OS; RG_MAC_APPS moves the system Applications root off the real one.
const findDriver = `
import os, sys
sys.path.insert(0, os.environ["RG_DIR"])
import review3
if os.environ.get("RG_PLATFORM"):
    review3.sys.platform = os.environ["RG_PLATFORM"]
if os.environ.get("RG_OSNAME"):
    review3.os.name = os.environ["RG_OSNAME"]
review3.MAC_APPS = os.environ["RG_MAC_APPS"]
print(review3.find_codex(None) or review3.codex_missing())
`

// copyRunner copies review3.py and prompt.md into a temp dir, so Go's
// test cache keys on the script's content, and returns the dir.
func copyRunner(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := t.TempDir()
	for _, name := range []string{"review3.py", "prompt.md"} {
		b, err := os.ReadFile(filepath.Join(skillDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// place writes an executable stub at path, making its parents.
func place(t *testing.T, path string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// find runs findDriver with HOME at home and an empty PATH, plus env.
func find(t *testing.T, runner, home string, env ...string) string {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	cmd := exec.Command(py, "-c", findDriver)
	cmd.Env = append([]string{
		"HOME=" + home,
		"PATH=" + t.TempDir(),
		"RG_DIR=" + runner,
		"RG_MAC_APPS=" + filepath.Join(home, "no-system-apps"),
		"RG_PLATFORM=linux",
	}, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCodexFoundOffPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stubs are POSIX executables")
	}
	runner := copyRunner(t)
	nvmBin := func(home, v string) string {
		return filepath.Join(home, ".nvm", "versions", "node", v, "bin", "codex")
	}

	t.Run("newest nvm version", func(t *testing.T) {
		// v22.10.0 sorts after v22.9.1 by number, before it by string.
		home := t.TempDir()
		place(t, nvmBin(home, "v20.1.0"), 0o700)
		place(t, nvmBin(home, "v22.9.1"), 0o700)
		want := place(t, nvmBin(home, "v22.10.0"), 0o700)
		if got := find(t, runner, home); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("skips a non-executable", func(t *testing.T) {
		home := t.TempDir()
		want := place(t, nvmBin(home, "v22.9.1"), 0o700)
		place(t, nvmBin(home, "v22.10.0"), 0o600)
		if got := find(t, runner, home); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("NVM_DIR moves nvm", func(t *testing.T) {
		home, nvm := t.TempDir(), t.TempDir()
		place(t, nvmBin(home, "v22.9.1"), 0o700)
		want := place(t, filepath.Join(nvm, "versions", "node", "v18.0.0", "bin", "codex"), 0o700)
		if got := find(t, runner, home, "NVM_DIR="+nvm); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("macOS app before nvm", func(t *testing.T) {
		home := t.TempDir()
		place(t, nvmBin(home, "v22.10.0"), 0o700)
		app := filepath.Join("ChatGPT.app", "Contents", "Resources", "codex-cli", "bin", "codex")
		sys := filepath.Join(home, "sys")
		want := place(t, filepath.Join(sys, app), 0o700)
		place(t, filepath.Join(home, "Applications", app), 0o700)
		got := find(t, runner, home, "RG_PLATFORM=darwin", "RG_MAC_APPS="+sys)
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		if got := find(t, runner, home, "RG_PLATFORM=darwin"); got != filepath.Join(home, "Applications", app) {
			t.Fatalf("user Applications: got %q", got)
		}
		if got := find(t, runner, home); got != nvmBin(home, "v22.10.0") {
			t.Fatalf("off macOS the app is not tried: got %q", got)
		}
	})
	t.Run("windows spots", func(t *testing.T) {
		home, nvmHome, appData := t.TempDir(), t.TempDir(), t.TempDir()
		place(t, filepath.Join(nvmHome, "v20.1.0", "codex.cmd"), 0o600)
		want := place(t, filepath.Join(nvmHome, "v22.10.0", "codex.cmd"), 0o600)
		npm := place(t, filepath.Join(appData, "npm", "codex.cmd"), 0o600)
		env := []string{"RG_OSNAME=nt", "APPDATA=" + appData}
		if got := find(t, runner, home, append(env, "NVM_HOME="+nvmHome)...); got != want {
			t.Fatalf("nvm-windows: got %q, want %q", got, want)
		}
		if got := find(t, runner, home, env...); got != npm {
			t.Fatalf("npm global: got %q, want %q", got, npm)
		}
	})
	t.Run("not found names the spots", func(t *testing.T) {
		home := t.TempDir()
		got := find(t, runner, home)
		if !strings.HasPrefix(got, "codex not found") ||
			!strings.Contains(got, filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "codex")) {
			t.Fatalf("got %q", got)
		}
	})
}

// reviewStart is step 5c of the pre-commit hook, the review's start,
// cut from the hook so the test runs the hook's own text.
func reviewStart(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../.githooks/pre-commit")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "# ---- 5c.")
	end := strings.Index(s, "# ---- 6.")
	if start < 0 || end < start {
		t.Fatal("pre-commit has no step 5c before step 6")
	}
	return s[start:end]
}

// With neither Codex nor the Claude CLI the hook has no reviewer: it warns
// and lets the commit through, and never fails it.
func TestHookWarnsWithoutReviewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a POSIX shell script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	// The repo under test: a staged Go file and the runner where the hook
	// looks for it.
	repo := t.TempDir()
	skill := filepath.Join(repo, ".claude", "skills", "review-gate")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := copyRunner(t)
	for _, name := range []string{"review3.py", "prompt.md"} {
		b, err := os.ReadFile(filepath.Join(runner, name))
		if err != nil {
			t.Fatal(err)
		}
		// Off the real /Applications, so an installed ChatGPT app does
		// not answer.
		const sys = `MAC_APPS = "/Applications"`
		if name == "review3.py" && !strings.Contains(string(b), sys) {
			t.Fatalf("review3.py no longer says %s", sys)
		}
		b = []byte(strings.Replace(string(b), sys, `MAC_APPS = "/nonexistent"`, 1))
		if err := os.WriteFile(filepath.Join(skill, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// PATH holds only what step 5c calls, so no real codex or claude is on it.
	bin := t.TempDir()
	for name, target := range map[string]string{"python3": py, "git": gitBin} {
		if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	grep, err := exec.LookPath("grep")
	if err != nil {
		t.Skip("no grep")
	}
	if err := os.Symlink(grep, filepath.Join(bin, "grep")); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := []string{"PATH=" + bin, "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
	for _, args := range [][]string{{"init", "-q"}, {"add", "x.go"}} {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = repo
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	prelude := `
pass() { printf 'PASS %s\n' "$1"; }
warn() { printf 'WARN %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1"; }
commit_index=""
staged_git() { git "$@"; }
`
	hook := func(extra ...string) (int, string) {
		cmd := exec.Command(sh, "-c", prelude+reviewStart(t)+"\necho REACHED-STEP-6\n")
		cmd.Dir = repo
		cmd.Env = append(append([]string{}, env...), extra...)
		out, err := cmd.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), string(out)
		} else if err != nil {
			t.Fatalf("sh: %v\n%s", err, out)
		}
		return 0, string(out)
	}

	code, out := hook()
	if code != 0 || !strings.Contains(out, "WARN model review skipped: codex not found") ||
		!strings.Contains(out, "claude not found either") || !strings.Contains(out, "subagent mode") ||
		!strings.Contains(out, "REACHED-STEP-6") {
		t.Fatalf("no reviewer: exit %d, want 0, a warning and step 6\n%s", code, out)
	}
	// A claude on PATH is the reviewer when there is no codex: the hook
	// starts the review instead of warning. The stub uses shell builtins
	// only and leaves a marker, so the test sees that the runs happened
	// and not just the hook's "started" line; the hook itself needs mktemp
	// for the review's output file.
	mktemp, err := exec.LookPath("mktemp")
	if err != nil {
		t.Skip("no mktemp")
	}
	if err := os.Symlink(mktemp, filepath.Join(bin, "mktemp")); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(bin, "claude")
	ran := filepath.Join(t.TempDir(), "ran")
	stub := "#!/bin/sh\nwhile read -r _; do :; done\necho run >> '" + ran + "'\necho '{\"decision\":\"approve\",\"findings\":[]}'\n"
	if err := os.WriteFile(claude, []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, "-c", prelude+reviewStart(t)+`
wait "$review_pid"; echo "REVIEW-EXIT $?"
while read -r line; do echo "$line"; done < "$review_out"
`)
	cmd.Dir = repo
	cmd.Env = env
	fallback, err := cmd.CombinedOutput()
	out = string(fallback)
	if err != nil || !strings.Contains(out, "Model review started (three runs of "+claude) ||
		strings.Contains(out, "WARN model review") || !strings.Contains(out, "REVIEW-EXIT 0") ||
		!strings.Contains(out, "review-gate: approve (0/3 usable runs request changes)") {
		t.Fatalf("claude fallback: %v, want three approving runs\n%s", err, out)
	}
	if b, err := os.ReadFile(ran); err != nil || strings.Count(string(b), "run") != 3 {
		t.Fatalf("the claude stub ran %q (%v), want three runs", b, err)
	}
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	code, out = hook("GOFASTR_SKIP_COMMIT_REVIEW=1")
	if code != 0 || !strings.Contains(out, "WARN model review skipped") || !strings.Contains(out, "REACHED-STEP-6") {
		t.Fatalf("skip: exit %d, want 0 and a warning\n%s", code, out)
	}
	// A --check that crashes is a review that could not run: the commit
	// goes through with a warning, as it does with no reviewer (exit 3).
	crash := "import sys\nprint('boom')\nsys.exit(1)\n"
	if err := os.WriteFile(filepath.Join(skill, "review3.py"), []byte(crash), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = hook()
	if code != 0 || !strings.Contains(out, "WARN model review skipped: review3.py --check exited 1: boom") || !strings.Contains(out, "REACHED-STEP-6") {
		t.Fatalf("crashed check: exit %d, want 0 and a warning\n%s", code, out)
	}
}
