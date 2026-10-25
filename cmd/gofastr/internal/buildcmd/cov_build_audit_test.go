package buildcmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── build.go ──────────────────────────────────────────────────────────

func covT_tinyModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module buildtest\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, dir)
	return dir
}

func TestRunBuildNoGenerate(t *testing.T) {
	dir := covT_tinyModule(t)
	clitest.CovT_capStdout(t, func() { RunBuild([]string{"--no-generate", "-o=" + filepath.Join(dir, "out", "server")}) })
	if _, err := os.Stat(filepath.Join(dir, "out", "server")); err != nil {
		t.Fatalf("binary not built: %v", err)
	}
}

func TestRunBuildOutputFlag(t *testing.T) {
	dir := covT_tinyModule(t)
	clitest.CovT_capStdout(t, func() { RunBuild([]string{"--no-generate", "--output=" + filepath.Join(dir, "bin2")}) })
	if _, err := os.Stat(filepath.Join(dir, "bin2")); err != nil {
		t.Fatalf("binary not built: %v", err)
	}
}
