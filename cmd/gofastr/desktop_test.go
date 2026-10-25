package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The `gofastr desktop` subcommand: usage routing, flag parsing, the
// bundle layout (plist + icns) on a tiny fixture, and --id validation.
// `types` is exercised against examples/desktop-notes in
// desktop_types_example_test.go (it compiles the real example).

func TestDesktopHelpIsCommandSpecific(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { dispatch([]string{"desktop", "--help"}) })
	if want := "Usage: gofastr desktop"; !strings.Contains(out, want) {
		t.Fatalf("desktop --help missing %q:\n%s", want, out)
	}
	for _, verb := range []string{"run", "build", "types"} {
		if !strings.Contains(out, verb) {
			t.Fatalf("desktop --help does not mention %q", verb)
		}
	}
	if strings.Contains(out, "Start dev server with auto-restart") {
		t.Fatal("desktop --help fell back to the global help page")
	}
}

func TestDesktopNoVerbPrintsUsageAndExits(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"desktop"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "Usage: gofastr desktop") {
		t.Fatalf("no usage printed:\n%s", out)
	}
}

func TestDispatchPassesDesktopBuildVersionToBuilder(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			dispatch([]string{"desktop", "build", "--id", "no-dot", "--version", "1.2.3"})
		})
	})
	if code != 1 || !strings.Contains(out, "reverse-DNS") {
		t.Fatalf("desktop build --version was intercepted as a global flag (exit %d):\n%s", code, out)
	}
}
