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

// ScaffoldEmptyDir sets up a temp dir with a go.mod (no app spec) and chdirs
// into it, the starting point for scaffolding into an empty project.
func ScaffoldEmptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	CovT_chdir(t, dir)
	WriteAddGoMod(t, dir, "example.com/scaffold")
	return dir
}
