package clitest

import (
	"os"
	"testing"
)

// Main runs a CLI test package under one short temp root that goes when
// the run ends. `gofastr dev` mints its build dir once per process
// (devcmd.DevServerBinDir) and removes it only on a clean shutdown, which
// in-process tests never reach; under this root it goes too. The root is
// short so service IDs fit native Unix socket path limits. Main removes
// the shared gofastr binary, runs cleanup, then exits with the tests'
// code. A package whose tests run `go vet` with the test binary as the
// vettool hands off (verifycmd.RunAsVettoolIfMarked) before calling Main.
func Main(m *testing.M, cleanup ...func()) {
	tmpRoot, err := os.MkdirTemp("", "gf-")
	if err == nil {
		for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
			_ = os.Setenv(k, tmpRoot)
		}
	}
	code := m.Run()
	if SharedGofastrBin.Dir != "" {
		_ = os.RemoveAll(SharedGofastrBin.Dir)
	}
	for _, f := range cleanup {
		f()
	}
	if tmpRoot != "" {
		_ = os.RemoveAll(tmpRoot)
	}
	os.Exit(code)
}
