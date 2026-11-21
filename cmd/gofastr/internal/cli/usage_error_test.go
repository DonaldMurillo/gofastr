package cli_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Fail is the CLI's error line, and errors belong on stderr
// (framework/docs/content/cli.md): `gofastr build 2>err.log` must catch
// the refusal, not the command's real stdout output.
func TestFailWritesToStderr(t *testing.T) {
	var errOut string
	out := clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() { cli.Fail("boom %d", 7) })
	})
	if strings.Contains(out, "boom") {
		t.Fatalf("Fail wrote to stdout:\n%s", out)
	}
	if !strings.Contains(errOut, "boom 7") {
		t.Fatalf("Fail's line missing on stderr:\n%s", errOut)
	}
}

// UsageFail is the one shared path for a command-line usage error: the
// ✗ line on stderr like Fail, then exit status 2, the status verify
// documents as "bad usage". --help output stays on stdout.
func TestUsageFailPrintsToStderrAndExitsTwo(t *testing.T) {
	var code int
	var errOut string
	clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() {
			code = clitest.CovT_capExit(t, func() { cli.UsageFail("unknown flag %s", "--zzz") })
		})
	})
	if code != 2 {
		t.Fatalf("UsageFail exited %d, want 2", code)
	}
	if !strings.Contains(errOut, "unknown flag --zzz") {
		t.Fatalf("UsageFail's line missing on stderr:\n%s", errOut)
	}
}
