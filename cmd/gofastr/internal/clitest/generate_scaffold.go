package clitest

import (
	"os/exec"
	"testing"
)

// BuildGenerated runs `go build ./...` in dir, failing the test on error.
func BuildGenerated(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-mod=mod", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated project did not build: %v\n%s", err, out)
	}
}

// ScaffoldEmptyDir chdirs into a new module with no app spec and returns it.
func ScaffoldEmptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	CovT_chdir(t, dir)
	WriteAddGoMod(t, dir, "example.com/scaffold")
	return dir
}
