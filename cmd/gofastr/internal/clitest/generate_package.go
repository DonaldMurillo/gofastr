package clitest

import (
	"path/filepath"
	"testing"
)

// WriteTestModule writes a go.mod that replaces the framework with the repo root.
func WriteTestModule(t *testing.T, dir string) {
	t.Helper()
	repoRoot := RepoRootDir(t)
	version, err := RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	WriteTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/pkgcopy\n\ngo "+version+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
}
