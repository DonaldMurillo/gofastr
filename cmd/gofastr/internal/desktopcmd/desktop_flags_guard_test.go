package desktopcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestDesktopBuildRefusesMisplacedSigningFlags drives each refusal the
// Windows signing and archive flags added: every case must exit 1 with
// its own message before any tool runs or dist/ appears.
func TestDesktopBuildRefusesMisplacedSigningFlags(t *testing.T) {
	for _, tc := range []struct {
		name, targetOS string
		args           []string
		want           string
	}{
		{"timestamp-url on linux", "linux", []string{"--timestamp-url=http://ts.example"}, "--timestamp-url and --zip are Windows build flags"},
		{"zip on darwin", "darwin", []string{"--zip"}, "--timestamp-url and --zip are Windows build flags"},
		{"notarize on windows", "windows", []string{"--notarize", "--sign=Acme"}, "Notarization is macOS-only"},
		{"entitlements on windows", "windows", []string{"--entitlements=e.plist"}, "Notarization is macOS-only"},
		{"timestamp-url without sign", "windows", []string{"--timestamp-url=http://ts.example"}, "--timestamp-url needs --sign"},
		{"zip with a two-part version", "windows", []string{"--zip", "--version=1.0"}, "to build an update archive with --zip"},
		{"zip with a four-part version", "windows", []string{"--zip", "--version=1.2.3.4"}, "to build an update archive with --zip"},
		// Valid semver, so only the 64-character cap refuses it.
		{"zip with an over-long version", "windows", []string{"--zip", "--version=1.0.0-" + strings.Repeat("a", 60)}, "to build an update archive with --zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := fakeRunTool(t, nil)
			wd := t.TempDir()
			oldWD, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(wd); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWD) })

			args := append([]string{"--id", "dev.gofastr.x", "--pkg", "./does/not/exist"}, tc.args...)
			var code int
			out := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() { runDesktopBuildForOS(args, tc.targetOS) })
			})
			if code != 1 {
				t.Fatalf("exit = %d, want 1:\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output does not carry %q:\n%s", tc.want, out)
			}
			if len(*calls) != 0 {
				t.Fatalf("tools ran before the refusal: %v", *calls)
			}
			if _, err := os.Stat(filepath.Join(wd, "dist")); err == nil {
				t.Fatal("dist/ created before the refusal")
			}
		})
	}
}
