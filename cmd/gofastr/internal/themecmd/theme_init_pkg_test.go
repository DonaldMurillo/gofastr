package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `theme init --out=theme.go` beside main.go wrote `package theme` into
// a package main directory, and the build failed on two packages.
func TestThemeInitAdoptsDirPackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "theme.go")
	clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--out=" + dest}) })
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\npackage main\n") {
		t.Fatalf("theme.go does not use package main:\n%.300s", got)
	}
}

// The default destination still writes package theme.
func TestThemeInitDefaultIsPackageTheme(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.CovT_capStdout(t, func() { runThemeInit(nil) })
	got, err := os.ReadFile(filepath.Join(dir, "theme", "theme.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "\npackage theme\n") {
		t.Fatalf("default theme.go is not package theme:\n%.300s", got)
	}
}
