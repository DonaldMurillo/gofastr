package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ----- (d) Help / usage exit codes -----

// BuildGofastrBin returns the package-wide shared gofastr binary
// (testbin_test.go), built once for the whole suite.
func BuildGofastrBin(t *testing.T) string {
	t.Helper()
	bin, err := clitest.GofastrTestBinary()
	if err != nil {
		t.Fatalf("go build: %v", err)
	}
	return bin
}

func TestNewNoArgsExitsNonZero(t *testing.T) {
	bin := BuildGofastrBin(t)
	cmd := exec.Command(bin, "new")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit on `new` with no resource, got 0; output: %s", out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "usage") {
		t.Fatalf("expected usage in output, got: %s", out)
	}
}

func TestNewHelpExitsZero(t *testing.T) {
	bin := BuildGofastrBin(t)
	cmd := exec.Command(bin, "new", "-h")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected zero exit on `new -h`, got %v; output: %s", err, out)
	}
	if !strings.Contains(strings.ToLower(string(out)), "usage") &&
		!strings.Contains(strings.ToLower(string(out)), "resources") {
		t.Fatalf("expected help text, got: %s", out)
	}
}

func TestNewCLIGeneratedHandlerBuilds(t *testing.T) {
	bin := BuildGofastrBin(t)
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	cmd := exec.Command(bin, "new", "handler", "Ping", "--method", "GET", "--path", "/ping")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr new handler: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ping_handler.go")); err != nil {
		t.Fatalf("handler not scaffolded: %v", err)
	}

	// `new entity` was removed. It must exit non-zero without scaffolding.
	ent := exec.Command(bin, "new", "entity", "User", "name:string")
	ent.Dir = dir
	if out, err := ent.CombinedOutput(); err == nil || !strings.Contains(string(out), "removed") {
		t.Fatalf("new entity should report removal and fail: err=%v out=%s", err, out)
	}
}
