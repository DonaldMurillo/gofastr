package permission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// quietRepo builds a working directory with a file, a subdirectory and
// a symlink that leads out of it, and returns an Engine running there.
func quietRepo(t *testing.T) (*Engine, string) {
	t.Helper()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "sub", "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	e := New(nil)
	e.WorkingDir = repo
	return e, repo
}

// The quiet preset's doc says Read/Glob/Ls under the repo, `cat
// <repo-file>` and `find . *`; the code auto-allowed any path, so
// Read:/proc/self/environ, ~/.ssh/id_rsa, `cat /proc/self/environ`,
// `grep -r . ~/.config/gofastr` and `find / -name creds.enc` all ran with
// no prompt. A path outside the working directory, after symlinks are
// resolved, now asks; so does one the checker cannot place.
func TestQuietModeAsksForReadsOutsideTheWorkingDir(t *testing.T) {
	e, repo := quietRepo(t)
	home, _ := os.UserHomeDir()
	sess := ids.NewSessionID()
	for _, c := range []struct{ tool, argv string }{
		{"Read", "Read:/proc/self/environ"},
		{"Read", "Read:~/.ssh/id_rsa"},
		{"Read", "Read:" + filepath.Join(home, ".ssh", "id_rsa")},
		{"Read", "Read:../outside.txt"},
		{"Read", "Read:sub/../../outside.txt"},
		{"Read", "Read:escape/secret"},
		{"Read", "Read"},
		{"Ls", "Ls:/"},
		{"Ls", "Ls:escape"},
		{"Glob", "Glob:*.go --root /etc"},
		{"Glob", "Glob:/etc/*"},
		{"Glob", "Glob:../**/*.go"},
		{"Glob", "Glob:* --root escape"},
		{"Grep", "Grep:TODO --path ~/.ssh"},
		{"Grep", "Grep:TODO --path /etc"},
		{"Grep", "Grep:TODO --glob ../*"},
		{"Bash", "cat /proc/self/environ"},
		{"Bash", "grep -r . ~/.config/gofastr"},
		{"Bash", "find / -name creds.enc"},
		{"Bash", "cat ../x"},
		{"Bash", "head -n 3 /etc/passwd"},
		{"Bash", "tail escape/secret"},
		{"Bash", "ls /"},
		{"Bash", "rg foo /etc"},
		{"Bash", "wc -l /etc/passwd"},
		{"Bash", "grep -f/etc/shadow x"},
		{"Bash", "grep --file=/etc/shadow x"},
		{"Bash", "cat esc*/secret"},
		{"Bash", "cat " + filepath.Join(home, ".ssh", "id_rsa")},
	} {
		if got := e.Evaluate(sess, c.tool, c.argv, c.tool == "Bash"); got != DecisionAsk {
			t.Errorf("SECURITY: quiet mode %s %q → %s, want ask (outside %s)", c.tool, c.argv, got, repo)
		}
	}
}

// The reads quiet mode exists for still pass without a prompt.
func TestQuietModeAllowsReadsInsideTheWorkingDir(t *testing.T) {
	e, repo := quietRepo(t)
	sess := ids.NewSessionID()
	for _, c := range []struct{ tool, argv string }{
		{"Read", "Read:go.mod"},
		{"Read", "Read:./sub/a.go"},
		{"Read", "Read:" + filepath.Join(repo, "go.mod")},
		{"Read", "Read:sub/missing.go"},
		{"Ls", "Ls"},
		{"Ls", "Ls:."},
		{"Ls", "Ls:sub"},
		{"Glob", "Glob:**/*.go"},
		{"Glob", "Glob:*.go --root sub"},
		{"Grep", "Grep:TODO"},
		{"Grep", "Grep:'two words' --path sub --glob *.go"},
		{"Bash", "cat go.mod"},
		{"Bash", "head -n 5 go.mod"},
		{"Bash", "grep -R foo ."},
		{"Bash", "find . -name *.go"},
		{"Bash", "find . -type f -name '*_test.go'"},
		{"Bash", "ls -la"},
		{"Bash", "ls sub"},
		{"Bash", "rg -n TODO ./sub"},
		{"Bash", "wc -l go.mod"},
		{"Bash", "cat sub/*.go"},
		{"Bash", "git status"},
		{"Bash", "pwd"},
	} {
		if got := e.Evaluate(sess, c.tool, c.argv, c.tool == "Bash"); got != DecisionAllow {
			t.Errorf("quiet mode %s %q → %s, want allow (inside %s)", c.tool, c.argv, got, repo)
		}
	}
}
