package initcmd

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ---- `gofastr init`: the emitted app must compile ----

// TestRunInitGeneratesBuildableApp: init's main.go template is the file
// users compile first, and it shared no build proof with the app spec
// path (cov_init_test.go asserts the file set and main.go strings
// only). Run the real generation, pin the module to this repo with a
// replace (the harness the app spec build test uses), and build every
// package it emitted.
func TestRunInitGeneratesBuildableApp(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.CovT_capStdout(t, func() { RunInit([]string{"myapp"}) })
	clitest.WriteAddGoMod(t, filepath.Join(dir, "myapp"), "local/myapp")
	clitest.BuildGenerated(t, filepath.Join(dir, "myapp"))
}
