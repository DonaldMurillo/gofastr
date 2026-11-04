package desktopcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFailedRunRemovesBuildDir(t *testing.T) {
	if os.Getenv("GOFASTR_TEST_FAILED_RUN") == "1" {
		runDesktopRun([]string{"--dir", os.Getenv("GOFASTR_TEST_PROJECT")})
		return
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module test.invalid/broken\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\nfunc main( broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestFailedRunRemovesBuildDir$")
	child.Env = append(os.Environ(), "GOFASTR_TEST_FAILED_RUN=1", "GOFASTR_TEST_PROJECT="+project, "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
	output, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("broken build succeeded: %s", output)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "gofastr-dev-server-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("failed run left build directories: %v", leftovers)
	}
}
