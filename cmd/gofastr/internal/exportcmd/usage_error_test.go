package exportcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A flag export does not take, or a missing output directory, is a usage
// error: the ✗ line goes to stderr and the exit status is 2. Both used
// to exit 1, indistinguishable from a failed export run.
func TestExportUsageErrorsExitTwoOnStderr(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":     {"--zzz-unknown-flag=1"},
		"missing dir":      {"--pkg=./x"},
		"flag needs value": {"--pkg"},
	} {
		var code int
		var errOut string
		clitest.CovT_capStdout(t, func() {
			errOut = clitest.CovT_capStderr(t, func() {
				code = clitest.CovT_capExit(t, func() { RunExport(args) })
			})
		})
		if code != 2 {
			t.Errorf("export %s: exit %d, want 2", name, code)
		}
		if !strings.Contains(errOut, "✗") {
			t.Errorf("export %s: diagnostic missing on stderr:\n%s", name, errOut)
		}
	}
}
