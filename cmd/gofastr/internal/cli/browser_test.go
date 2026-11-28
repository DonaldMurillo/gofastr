package cli_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestOpenBrowserWithoutAnOpenerPrintsAHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out string
	errOut := clitest.CovT_capStderr(t, func() {
		out = clitest.CovT_capStdout(t, func() { cli.OpenBrowser("http://127.0.0.1:1/") })
	})
	if out != "" || !strings.Contains(errOut, "could not auto-open browser") {
		t.Fatalf("stdout %q, stderr %q: want the hint on stderr only", out, errOut)
	}
}
