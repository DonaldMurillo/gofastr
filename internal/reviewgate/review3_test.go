package reviewgate

//gofastr:test-reads .claude/skills/review-gate/

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const skillDir = "../../.claude/skills/review-gate"

// stubCodex is a codex that answers run N with the file answerN.json
// beside it, picked from the run<N>.txt name review3.py passes to -o.
const stubCodex = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out=$2; shift; done
cat >/dev/null
n=$(basename "$out" .txt | sed 's/^run//')
cat "$(dirname "$0")/answer$n.json" > "$out"
`

// setup copies the runner into a temp dir (so Go's test cache keys on
// the script's content), writes the stub codex and its three answers, and
// makes a repo with one staged change. It returns the script, the stub
// and the repo.
func setup(t *testing.T, answers [3]string) (script, codex, repo string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub codex is a shell script")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
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
	codex = filepath.Join(dir, "codex")
	if err := os.WriteFile(codex, []byte(stubCodex), 0o700); err != nil {
		t.Fatal(err)
	}
	for i, a := range answers {
		name := filepath.Join(dir, "answer"+string(rune('1'+i))+".json")
		if err := os.WriteFile(name, []byte(a), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repo = t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "x.go"},
	} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(repo, "x.go"), []byte("package x\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = gitless(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return filepath.Join(dir, "review3.py"), codex, repo
}

// gitless drops git's per-repo variables from env. Under a git hook, or a
// reviewer started by one, GIT_INDEX_FILE names the commit's index, and a
// fixture's git add would write into it.
func gitless(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY",
			"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_PREFIX":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// run runs review3.py --staged in dir and returns its exit code and output.
func run(t *testing.T, script, codex, dir string) (int, string) {
	t.Helper()
	cmd := exec.Command("python3", script, "--staged", "--codex", codex, "--timeout", "60")
	cmd.Dir = dir
	cmd.Env = append(gitless(os.Environ()), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exit):
		return exit.ExitCode(), string(out)
	default:
		t.Fatalf("run: %v\n%s", err, out)
		return 0, ""
	}
}

const (
	approve = `{"decision":"approve","findings":[]}`
	block   = `{"decision":"request_changes","findings":[{"path":"x.go","line":42,"blocking":true,"issue":"wrong"}]}`
)

func TestVoteDecidesTheExitCode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers [3]string
		want    int
	}{
		{"three blocks exit 1", [3]string{block, block, block}, 1},
		{"two blocks approve", [3]string{block, block, approve}, 0},
		{"one block approves", [3]string{approve, block, approve}, 0},
		{"three approves", [3]string{approve, approve, approve}, 0},
		{"unusable run is incomplete", [3]string{block, block, "not json"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script, codex, repo := setup(t, tc.answers)
			if got, out := run(t, script, codex, repo); got != tc.want {
				t.Fatalf("exit %d, want %d\n%s", got, tc.want, out)
			}
		})
	}
}

// A model writes the line as 42, "42" or null. Sorting the merged
// findings once raised TypeError, and the traceback's exit 1 read as a
// block.
func TestMixedLineTypesMerge(t *testing.T) {
	str := strings.Replace(block, `"line":42`, `"line":"42"`, 1)
	null := strings.Replace(block, `"line":42`, `"line":null`, 1)
	script, codex, repo := setup(t, [3]string{block, str, null})
	got, out := run(t, script, codex, repo)
	if got != 1 {
		t.Fatalf("exit %d, want 1\n%s", got, out)
	}
	if !strings.Contains(out, "x.go:42 (runs 1, 2)") {
		t.Fatalf("42 and \"42\" not merged:\n%s", out)
	}
}

func TestFailuresNeverExitOne(t *testing.T) {
	t.Run("codex will not start", func(t *testing.T) {
		script, codex, repo := setup(t, [3]string{block, block, block})
		if err := os.Chmod(codex, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, out := run(t, script, codex, repo); got != 2 {
			t.Fatalf("exit %d, want 2\n%s", got, out)
		}
	})
	t.Run("codex exits nonzero", func(t *testing.T) {
		// A run that wrote an answer and then failed is not a vote.
		script, codex, repo := setup(t, [3]string{block, block, block})
		failing := strings.Replace(stubCodex, `> "$out"`, `> "$out"; exit 1`, 1)
		if err := os.WriteFile(codex, []byte(failing), 0o700); err != nil {
			t.Fatal(err)
		}
		if got, out := run(t, script, codex, repo); got != 2 {
			t.Fatalf("exit %d, want 2\n%s", got, out)
		}
	})
	t.Run("git fails", func(t *testing.T) {
		script, codex, _ := setup(t, [3]string{block, block, block})
		notRepo := t.TempDir()
		cmd := exec.Command("python3", script, "--staged", "--codex", codex)
		cmd.Dir = notRepo
		cmd.Env = append(gitless(os.Environ()), "GIT_CEILING_DIRECTORIES="+filepath.Dir(notRepo))
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("err %v, want exit 2\n%s", err, out)
		}
	})
}

func TestCodexRunsWithoutGitEnv(t *testing.T) {
	// The hook passes the commit's index in GIT_INDEX_FILE. review3.py
	// reads the diff through it, and must not hand it to codex.
	script, codex, repo := setup(t, [3]string{approve, approve, approve})
	recording := strings.Replace(stubCodex, "cat >/dev/null\n",
		"cat >/dev/null\nenv > \"$(dirname \"$0\")/env.txt\"\n", 1)
	if err := os.WriteFile(codex, []byte(recording), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", script, "--staged", "--codex", codex, "--timeout", "60")
	cmd.Dir = repo
	cmd.Env = append(gitless(os.Environ()), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_INDEX_FILE="+filepath.Join(repo, ".git", "index"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("review3.py: %v\n%s", err, out)
	}
	env, err := os.ReadFile(filepath.Join(filepath.Dir(codex), "env.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(env), "GIT_INDEX_FILE=") {
		t.Fatalf("codex ran with GIT_INDEX_FILE:\n%s", env)
	}
}

// stubClaude is a claude that prints answer.json from beside itself and
// records the arguments it was started with.
const stubClaude = `#!/bin/sh
dir=$(dirname "$0")
cat > /dev/null
echo "$@" >> "$dir/args.txt"
cat "$dir/answer.json"
`

// Without codex the three runs go to the Claude CLI on Haiku, read-only,
// and its votes decide the exit code like codex's do.
func TestClaudeReviewsWhenCodexIsMissing(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		want         int
	}{
		{"three blocks exit 1", block, 1},
		{"three approvals exit 0", approve, 0},
		{"no answer exits 2", "not json", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script, codex, repo := setup(t, [3]string{approve, approve, approve})
			dir := filepath.Dir(codex)
			// Off the real /Applications, so an installed ChatGPT app's
			// codex does not answer on macOS.
			src, err := os.ReadFile(script)
			if err != nil {
				t.Fatal(err)
			}
			const sys = `MAC_APPS = "/Applications"`
			if !strings.Contains(string(src), sys) {
				t.Fatalf("review3.py no longer says %s", sys)
			}
			if err := os.WriteFile(script, []byte(strings.Replace(string(src), sys, `MAC_APPS = "/nonexistent"`, 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			claude := filepath.Join(dir, "claude")
			if err := os.WriteFile(claude, []byte(stubClaude), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "answer.json"), []byte(tc.answer), 0o600); err != nil {
				t.Fatal(err)
			}
			py, err := exec.LookPath("python3")
			if err != nil {
				t.Skip("no python3")
			}
			gitBin, err := exec.LookPath("git")
			if err != nil {
				t.Skip("no git")
			}
			// No codex anywhere review3.py looks: an empty HOME, and a
			// PATH with git and the two tools the stub calls.
			bin := t.TempDir()
			for name, target := range map[string]string{"git": gitBin, "cat": "", "dirname": ""} {
				if target == "" {
					if target, err = exec.LookPath(name); err != nil {
						t.Skipf("no %s", name)
					}
				}
				if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(py, script, "--staged", "--claude", claude, "--timeout", "60")
			cmd.Dir = repo
			cmd.Env = []string{"PATH=" + bin, "HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1"}
			out, err := cmd.CombinedOutput()
			got := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				got = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("review3.py: %v\n%s", err, out)
			}
			if got != tc.want {
				t.Fatalf("exit %d, want %d\n%s", got, tc.want, out)
			}
			args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
			if err != nil {
				t.Fatalf("claude never ran: %v\n%s", err, out)
			}
			if n := strings.Count(string(args), "--model haiku"); n != 3 {
				t.Fatalf("%d runs on haiku, want 3:\n%s", n, args)
			}
			for _, want := range []string{"--permission-mode dontAsk", "--tools Read,Grep,Glob "} {
				if !strings.Contains(string(args), want) {
					t.Fatalf("claude ran without %q:\n%s", want, args)
				}
			}
			// No tool that can write: Bash counts, since
			// `git diff --output=<path>` writes a file.
			for _, banned := range []string{"Edit", "Write", "Bash"} {
				if strings.Contains(string(args), banned) {
					t.Fatalf("claude was given %s:\n%s", banned, args)
				}
			}
		})
	}
}

// A CLAUDE_BIN that names a file nobody can execute is not a reviewer:
// --check must not report it, or the hook would start runs that cannot.
func TestNonExecutableClaudeIsNotFound(t *testing.T) {
	script, codex, _ := setup(t, [3]string{approve, approve, approve})
	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(strings.Replace(string(src), `MAC_APPS = "/Applications"`, `MAC_APPS = "/nonexistent"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(filepath.Dir(codex), "claude-stale")
	if err := os.WriteFile(stale, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	cmd := exec.Command(py, script, "--check")
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir(), "CLAUDE_BIN=" + stale}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err %v, want exit 3\n%s", err, out)
	}
}
