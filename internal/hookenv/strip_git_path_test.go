package hookenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeGit writes a git into dir that answers --exec-path with execPath.
func fakeGit(t *testing.T, dir, execPath string) {
	t.Helper()
	script := "#!/bin/sh\n[ \"$1\" = --exec-path ] && echo '" + execPath + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

// source runs strip-git-path.sh under path, then tail, and returns what
// tail prints. The test reads the script itself, not sh, so Go's test cache
// keys on its content.
func source(t *testing.T, path, tail string) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	script, err := os.ReadFile("strip-git-path.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, "-c", string(script)+"\n"+tail)
	cmd.Env = []string{"PATH=" + path}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sh: %v\n%s", err, out)
	}
	return string(out)
}

// pathAfter returns the PATH strip-git-path.sh leaves behind.
func pathAfter(t *testing.T, path string) string {
	t.Helper()
	return source(t, path, `printf %s "$PATH"`)
}

func TestStripGitPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hooks run under sh")
	}
	execDir := t.TempDir() // git's libexec, prepended by git
	binDir := t.TempDir()  // where the user's git lives
	empty := t.TempDir()
	fakeGit(t, binDir, execDir)

	t.Run("drops the libexec prefix", func(t *testing.T) {
		if got, want := pathAfter(t, execDir+":"+binDir), binDir; got != want {
			t.Fatalf("PATH = %q, want %q", got, want)
		}
	})
	t.Run("keeps it when git lives only there", func(t *testing.T) {
		fakeGit(t, execDir, execDir)
		t.Cleanup(func() { os.Remove(filepath.Join(execDir, "git")) })
		path := execDir + ":" + empty
		if got := pathAfter(t, path); got != path {
			t.Fatalf("PATH = %q, want it unchanged %q", got, path)
		}
	})
	t.Run("leaves a later libexec entry alone", func(t *testing.T) {
		path := binDir + ":" + execDir
		if got := pathAfter(t, path); got != path {
			t.Fatalf("PATH = %q, want it unchanged %q", got, path)
		}
	})
	t.Run("leaves no variable behind", func(t *testing.T) {
		if got := source(t, execDir+":"+binDir, `printf %s "${stripped-unset}"`); got != "unset" {
			t.Fatalf("stripped = %q after sourcing, want it unset", got)
		}
	})
}
