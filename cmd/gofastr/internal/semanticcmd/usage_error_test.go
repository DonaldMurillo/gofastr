package semanticcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// An unknown subcommand or an unknown query flag is a usage error: the
// ✗ line goes to stderr and the exit status is 2, like every other
// gofastr command. Both used to exit 1.
func TestSemanticUsageErrorsExitTwoOnStderr(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown subcommand": {"bogus-sub"},
		"unknown query flag": {"query", "--zzz-unknown-flag=1"},
		"missing query":      {"query"},
	} {
		var code int
		var errOut string
		clitest.CovT_capStdout(t, func() {
			errOut = clitest.CovT_capStderr(t, func() {
				code = clitest.CovT_capExit(t, func() { RunSemantic(args) })
			})
		})
		if code != 2 {
			t.Errorf("semantic %s: exit %d, want 2", name, code)
		}
		if !strings.Contains(errOut, "✗") {
			t.Errorf("semantic %s: diagnostic missing on stderr:\n%s", name, errOut)
		}
	}
}
