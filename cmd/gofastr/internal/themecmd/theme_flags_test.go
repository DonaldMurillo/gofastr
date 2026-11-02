package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `theme init --out path` and `theme edit --addr host:port` answered
// "Unknown flag: --out" / "Unknown flag: --addr" about flags that exist,
// because the hand-rolled loops read only the --flag=value spelling.
// Every other command takes both (cli.ParseFlags).
func TestThemeInitTakesSpaceSeparatedOut(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	code := -1
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { runThemeInit([]string{"--out", "custom/t.go", "-f"}) })
	})
	if code != -1 {
		t.Fatalf("theme init --out custom/t.go exited %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join("custom", "t.go")); err != nil {
		t.Fatalf("theme init --out custom/t.go wrote nothing there: %v\n%s", err, out)
	}
}

func TestThemeEditTakesSpaceSeparatedAddr(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	// A non-loopback address is refused before anything binds, so the
	// refusal proves --addr's value was read.
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { runThemeEdit([]string{"--addr", "192.0.2.1:0", "--no-open"}) })
	})
	if code != 1 || !strings.Contains(out, `refusing to bind the theme editor to "192.0.2.1:0"`) {
		t.Fatalf("theme edit --addr 192.0.2.1:0: exit %d, want the non-loopback refusal:\n%s", code, out)
	}
}

func TestThemeEditRefusesMissingAddrValue(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { runThemeEdit([]string{"--addr", "--no-open"}) })
	})
	if code != 1 || !strings.Contains(out, "--addr needs a value") {
		t.Fatalf("theme edit --addr --no-open: exit %d:\n%s", code, out)
	}
}
