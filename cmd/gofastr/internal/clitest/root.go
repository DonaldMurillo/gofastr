package clitest

import (
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/coverageroot"
)

var moduleRoot = findModuleRoot()

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, _ := coverageroot.FindUp(dir, "go.mod")
	return root
}

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
