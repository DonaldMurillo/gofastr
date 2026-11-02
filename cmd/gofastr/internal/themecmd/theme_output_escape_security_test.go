package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// hostileDir is a directory name a cloned repo can carry: an OSC 52
// clipboard write, a screen clear, the 8-bit CSI spelling and a bidi
// override.
const hostileDir = "t\x1b]52;c;cHduZWQ=\x07\x1b[2J\u009b31m\u202e"

// `theme edit` and `theme init` printed projectfs refusals and the paths
// they name with a raw fmt.Printf, so a checkout's directory name
// (theme edit --out=<it>/theme.go, with <it> linked out of the project)
// drove the operator's terminal. They go through the status helpers,
// which scrub every argument.
func TestThemeCommandsPrintRepoPathsInert(t *testing.T) {
	old := cli.StdoutIsTTY
	cli.StdoutIsTTY = false // no helper styling: every escape would be the payload's
	t.Cleanup(func() { cli.StdoutIsTTY = old })
	base := t.TempDir()
	proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "outside")
	for _, d := range []string{proj, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(proj, hostileDir)); err != nil {
		t.Skipf("file system refuses the payload name: %v", err)
	}
	clitest.CovT_chdir(t, proj)
	out := hostileDir + "/theme.go"
	for name, run := range map[string]func(){
		// The non-loopback --addr is refused too, so a broken Check exits
		// rather than serving.
		"theme edit": func() { runThemeEdit([]string{"--no-open", "--addr=0.0.0.0:9", "--out=" + out}) },
		"theme init": func() { runThemeInit([]string{"--out=" + out}) },
		"theme init --force": func() {
			runThemeInit([]string{"--force", "--out=" + out})
		},
	} {
		var code int
		printed := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, run) })
		if code != 1 {
			t.Errorf("%s through a linked %q exited %d, want 1:\n%q", name, hostileDir, code, printed)
		}
		if !strings.Contains(printed, "cHduZWQ=") {
			t.Fatalf("setup broken: %s printed no part of the payload:\n%q", name, printed)
		}
		for i, r := range printed {
			if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
				t.Errorf("SECURITY: [theme-output-escape] %s printed %U at byte %d live to the terminal:\n%q", name, r, i, printed)
				break
			}
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Errorf("a theme command wrote %d entries outside the project", len(entries))
	}
}
