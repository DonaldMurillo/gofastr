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
		clitest.CovT_capOutput(t, func() { RunGenerate([]string{"ts"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunGenerateUnknownResourceExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capOutput(t, func() { RunGenerate([]string{"frobnicate"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunGenerateAllIsNotAGenerator(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunGenerate([]string{"all"}) })
	})
	if code != 2 || !strings.Contains(out, "Unknown generator: all") {
		t.Fatalf("generate all: exit %d, out %q; want the unknown-generator refusal", code, out)
	}
}

func TestRunGenerateUsageErrorsExitTwo(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"project", []string{"--out", "-x"}, "write --out=<value>"},
		{"watch", []string{"--watch", "--out", "-x"}, "write --out=<value>"},
		{"app", []string{"app", "--out", "-x"}, "write --out=<value>"},
		{"app conflicting flags", []string{"app", "--add", "--force"}, "mutually exclusive"},
		{"entity", []string{"entity", "post", "--out", "-x"}, "write --out=<value>"},
		{"screen", []string{"screen", "home", "--out", "-x"}, "write --out=<value>"},
		{"package", []string{"package", "siteheader", "--out", "-x"}, "write --out=<value>"},
		{"cli", []string{"cli", "--out", "-x"}, "write --out=<value>"},
		{"sdk", []string{"sdk", "--out", "-x"}, "write --out=<value>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := clitest.CovT_capOutput(t, func() {
				code = clitest.CovT_capExit(t, func() { RunGenerate(tc.args) })
			})
			if code != 2 || !strings.Contains(out, tc.want) {
				t.Fatalf("exit %d, output %q; want exit 2 and %q", code, out, tc.want)
			}
		})
	}
}

func TestRunGenerateAppSpecDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "bp.yml")
	if err := os.WriteFile(bp, []byte(clitest.TestAppSpecYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capOutput(t, func() {
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
