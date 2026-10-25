package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

// moduleRoot is the repo's module root, found at package init by walking
// up from the working directory to go.mod. Init runs before any test
// chdirs, and the walk does not care how deep the calling test package
// sits under cmd/gofastr. It is empty when no go.mod is found: a test
// that re-executes its own binary from a temp directory never reads it,
// and must not die at init looking for it. Readers go through
// mustModuleRoot or RepoRootDir.
var moduleRoot = findModuleRoot()

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// mustModuleRoot returns moduleRoot, panicking when init found none.
func mustModuleRoot() string {
	if moduleRoot == "" {
		panic("clitest: no go.mod at or above the test's working directory")
	}
	return moduleRoot
}

// RepoRootDir returns the repo's module root as an absolute path.
func RepoRootDir(t *testing.T) string {
	t.Helper()
	if moduleRoot == "" {
		t.Fatal("clitest: no go.mod at or above the test's working directory")
	}
	return moduleRoot
}
