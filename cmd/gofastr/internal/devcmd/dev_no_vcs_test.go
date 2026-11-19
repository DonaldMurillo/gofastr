package devcmd

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// The dev loop's go list and go build stamped version control info, so
// every save ran git status in the app's repository. That took the
// repository's index.lock while the developer's own git commands ran,
// and a dev server killed mid-query left the lock behind for good.
func TestDevGoCommandsRunNoGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the git recorder is a shell script")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":  "module example.com/vcs\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "x"}} {
		cmd := exec.Command(realGit, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	recorder := "#!/bin/sh\necho \"$@\" >> \"$GIT_RECORDER_CALLS\"\nexec \"$GIT_RECORDER_REAL\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(recorder), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_RECORDER_CALLS", calls)
	t.Setenv("GIT_RECORDER_REAL", realGit)
	t.Setenv("GOFLAGS", "")
	t.Setenv("TMPDIR", t.TempDir())

	noGit := func(step string) {
		t.Helper()
		if got, err := os.ReadFile(calls); err == nil {
			t.Errorf("%s ran git in the app's repository:\n%s", step, got)
			_ = os.Remove(calls)
		}
	}
	if _, err := listDeps(context.Background(), root, ".", nil); err != nil {
		t.Fatalf("listDeps: %v", err)
	}
	noGit("the watch's go list -deps")
	if name := packageName(root, "."); name != "main" {
		t.Fatalf("packageName = %q", name)
	}
	noGit("the package-name go list")
	var cmd *exec.Cmd
	if !buildGatesAndBinary(root, ".", nil, &sync.Mutex{}, &cmd, true, io.Discard, nil) {
		t.Fatal("the dev build failed")
	}
	noGit("the dev go build")
}
