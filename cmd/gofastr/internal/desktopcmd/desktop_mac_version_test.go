package desktopcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The macOS build wrote --version into Info.plist unchecked. The updater
// compares CFBundleShortVersionString as semver, so a bundle built with
// "1.0" failed every update check forever. The build refuses it first.
func TestMacBuildRefusesNonSemverVersion(t *testing.T) {
	for _, v := range []string{"1.0", "1.2.3+build", "01.2.3", "1.2.3-"} {
		t.Run(v, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "dist")
			var code int
			captured := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() {
					runDesktopBuildForOS([]string{"--id", "dev.example.notes", "--version", v, "--pkg", "./does/not/exist", "-o", out}, "darwin")
				})
			})
			if code != 1 {
				t.Fatalf("exit = %d, want 1\n%s", code, captured)
			}
			if !strings.Contains(captured, "--version") {
				t.Fatalf("error does not name --version:\n%s", captured)
			}
			if _, err := os.Stat(out); err == nil {
				t.Fatal("output dir created for an invalid --version")
			}
		})
	}
}
