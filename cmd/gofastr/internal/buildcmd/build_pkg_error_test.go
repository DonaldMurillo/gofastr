package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestBuildPkgNonMainFailureNamesTarget(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module buildpkgtest\n\ngo 1.21\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "lib", "lib.go"), "package lib\n")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			RunBuild([]string{"--no-generate", "--no-a11y", "--pkg=./lib"})
		})
	})
	if code != 1 {
		t.Fatalf("non-main target exit = %d, want 1", code)
	}
	if !strings.Contains(out, `Build target "./lib" is invalid`) {
		t.Fatalf("diagnostic does not name target:\n%s", out)
	}
}
