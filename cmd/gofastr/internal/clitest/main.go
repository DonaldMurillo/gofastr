package clitest

import (
	"os"
	"testing"
)

// Main runs a CLI test package under one short temp root, which it sets as
// TMPDIR, TMP and TEMP; after the tests it removes the root, the shared
// binary and scratch packages, runs cleanup, and exits with the tests' code.
func Main(m *testing.M, cleanup ...func()) {
	tmpRoot, err := os.MkdirTemp("", "gf-")
	if err == nil {
		runRoot = tmpRoot
		for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
			_ = os.Setenv(k, tmpRoot)
		}
	}
	code := m.Run()
	if SharedGofastrBin.Dir != "" {
		_ = os.RemoveAll(SharedGofastrBin.Dir)
	}
	removeSharedTrees()
	for _, f := range cleanup {
		f()
	}
	if tmpRoot != "" {
		_ = os.RemoveAll(tmpRoot)
	}
	os.Exit(code)
}
