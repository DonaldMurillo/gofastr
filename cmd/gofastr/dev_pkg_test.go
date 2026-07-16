package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

func devPkgWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// devPkgModule creates a module whose main package lives under cmd/app/ — the
// layout every non-scaffold app uses — with internal/ code the command depends
// on. The command writes a marker into its working directory so tests can
// assert where it actually ran.
func devPkgModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	devPkgWrite(t, filepath.Join(dir, "go.mod"), "module devtest\n\ngo 1.21\n")
	devPkgWrite(t, filepath.Join(dir, "internal", "greet", "greet.go"),
		"package greet\n\nfunc Msg() string { return \"hi\" }\n")
	devPkgWrite(t, filepath.Join(dir, "cmd", "app", "main.go"),
		"package main\n\nimport (\n\t\"os\"\n\t\"time\"\n\n\t\"devtest/internal/greet\"\n)\n\n"+
			"func main() {\n\t_ = os.WriteFile(\"ran-here.txt\", []byte(greet.Msg()), 0o644)\n\ttime.Sleep(time.Hour)\n}\n")
	return dir
}

// A cmd/-layout app must build via --pkg while the server's cwd stays at the
// project root. Before --pkg existed, the only way to build such an app was
// --dir ./cmd/app, which moved cwd too — so relative paths (sqlite db_url,
// static dirs) resolved against cmd/app/ and silently used the wrong files.
func TestBuildAndServeWithPkgKeepsProjectRootAsCwd(t *testing.T) {
	dir := devPkgModule(t)
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(devServerBinaryPath(rt)) })

	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	covT_capStdout(t, func() {
		ok = buildAndServe(dir, "./cmd/app", "localhost:0", rt, &mu, &cmd)
	})
	defer killServer(&mu, &cmd)

	if !ok {
		t.Fatal("buildAndServe(--pkg ./cmd/app) should succeed for a cmd/-layout module")
	}

	// The marker lands in the cwd the child was given. It must be the project
	// root, not the command's own directory.
	devPkgWaitForFile(t, filepath.Join(dir, "ran-here.txt"))
	if _, err := os.Stat(filepath.Join(dir, "cmd", "app", "ran-here.txt")); err == nil {
		t.Fatal("server ran with cwd=cmd/app; relative paths would resolve against the command dir")
	}
}

// The watch root is independent of the build target: editing internal/ must
// trigger a rebuild for a cmd/-layout app. --dir ./cmd/app would miss these.
func TestScanModTimesWatchesInternalForCmdLayout(t *testing.T) {
	dir := devPkgModule(t)
	prev := scanModTimes(dir)
	if len(prev) == 0 {
		t.Fatal("expected watched files at project root")
	}
	internal := filepath.Join(dir, "internal", "greet", "greet.go")
	if _, tracked := prev[internal]; !tracked {
		t.Fatalf("internal/ must be watched from the project root; tracked %d files, missing %s", len(prev), internal)
	}

	time.Sleep(10 * time.Millisecond)
	devPkgWrite(t, internal, "package greet\n\nfunc Msg() string { return \"edited\" }\n")
	if !changed(prev, scanModTimes(dir)) {
		t.Fatal("editing internal/ must trigger a rebuild")
	}
}

// Default --pkg is "." so the scaffold layout (main at the project root) keeps
// working exactly as before.
func TestBuildAndServePkgDefaultsToDir(t *testing.T) {
	dir := t.TempDir()
	devPkgWrite(t, filepath.Join(dir, "go.mod"), "module devtest\n\ngo 1.21\n")
	devPkgWrite(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"time\"\n\nfunc main() { time.Sleep(time.Hour) }\n")
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatalf("isolation.Resolve: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(devServerBinaryPath(rt)) })

	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	covT_capStdout(t, func() {
		ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd)
	})
	defer killServer(&mu, &cmd)
	if !ok {
		t.Fatal("root-layout module should still build with pkg=\".\"")
	}
}

func devPkgWaitForFile(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
