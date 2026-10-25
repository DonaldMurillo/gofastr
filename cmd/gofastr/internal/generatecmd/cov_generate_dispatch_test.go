package generatecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestRunGenerateTSRemovedExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunGenerate([]string{"ts"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunGenerateUnknownResourceExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunGenerate([]string{"frobnicate"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunGenerateEntityRemovedExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunGenerate([]string{"entity", "Widget", "name:string"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunGenerateAppSpecDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() {
		RunGenerate([]string{"app", bp, "--dry-run", "--json"})
	})
	if !strings.Contains(out, `"files"`) {
		t.Fatalf("expected JSON file list, got: %s", out)
	}
}

func TestProjectRelativePath(t *testing.T) {
	if projectRelativePath(".", "x.json") != "x.json" {
		t.Fatal("dot project dir passthrough")
	}
	abs := filepath.Join(t.TempDir(), "other")
	if projectRelativePath(filepath.Join(t.TempDir(), "project"), abs) != abs {
		t.Fatal("absolute path passthrough")
	}
	if projectRelativePath("proj", "x.json") != filepath.Join("proj", "x.json") {
		t.Fatal("relative join")
	}
}

// NOTE: runOnce / runGenerateWatch exec os.Executable() (the gofastr
// binary) and the watch loop polls forever. Exercising them in-process
// would re-enter the test binary or block. They are recorded as hard
// paths rather than forced.
