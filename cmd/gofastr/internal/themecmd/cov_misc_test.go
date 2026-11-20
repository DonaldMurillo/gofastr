package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/heldfiletest"
)

// ── theme.go ──────────────────────────────────────────────────────────

func TestRunThemeDispatch(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunTheme(nil) })
	if !strings.Contains(out, "Usage: gofastr theme") {
		t.Fatalf("help: %s", out)
	}
	out = clitest.CovT_capStdout(t, func() { RunTheme([]string{"help"}) })
	if !strings.Contains(out, "Subcommands") {
		t.Fatalf("help subcommand: %s", out)
	}
}

func TestRunThemeUnknownExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunTheme([]string{"frob"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunThemeInitWritesFile(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.CovT_capStdout(t, func() { runThemeInit(nil) })
	if _, err := os.Stat(filepath.Join(dir, "theme", "theme.go")); err != nil {
		t.Fatalf("theme.go not written: %v", err)
	}
	// Second run without --force → exits.
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runThemeInit(nil) })
	})
	if code != 1 {
		t.Fatalf("expected exit on existing, got %d", code)
	}
	// --force overwrites.
	clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--force"}) })
}

func TestRunThemeInitCustomOut(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--out=custom/t.go"}) })
	if _, err := os.Stat(filepath.Join(dir, "custom", "t.go")); err != nil {
		t.Fatalf("custom out not written: %v", err)
	}
}

func TestRunThemeInitHelp(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--help"}) })
	if !strings.Contains(out, "theme init") {
		t.Fatalf("help: %s", out)
	}
}

func TestRunThemeInitBadFlagExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runThemeInit([]string{"--nope"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

// `theme init --force` truncated the theme in place; it is renamed over
// whole.
func TestThemeInitForceReplacesWhole(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "theme.go")
	if err := os.WriteFile(dest, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := heldfiletest.Held(t, dest)
	clitest.CovT_capStdout(t, func() { RunTheme([]string{"init", "--out=" + dest, "--force"}) })
	if got := old(); got != "old" {
		t.Fatalf("the old handle reads %q: rewritten in place", got)
	}
	if got, _ := os.ReadFile(dest); !strings.Contains(string(got), "package") {
		t.Fatalf("theme not written: %q", got)
	}
}
