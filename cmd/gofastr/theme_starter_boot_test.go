package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The file `gofastr theme init` writes must boot: app.WithTheme runs
// Validate, which panics on any token left zero. Checking the literal
// for group names let five Durations go missing unnoticed, so this
// compiles the starter in a temp module and runs Validate on it.
func TestThemeStarterBootsValid(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := os.MkdirAll(filepath.Join(dir, "theme"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "theme", "theme.go"), themeStarter)
	writeTestFile(t, filepath.Join(dir, "theme", "boot_test.go"), `package theme

import "testing"

func TestStarterValidates(t *testing.T) {
	if err := App.Validate(); err != nil {
		t.Fatal(err)
	}
}
`)
	cmd := exec.Command("go", "test", "./theme")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := string(out)
		for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
			if strings.Contains(text, marker) {
				t.Skipf("temp module could not be built in this environment: %s", text)
			}
		}
		t.Fatalf("the theme init starter does not validate:\n%s", text)
	}
}
