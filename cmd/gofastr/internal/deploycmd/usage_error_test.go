package deploycmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A mistake on the command line is a usage error: diagnostic on stderr,
// exit status 2. It used to exit 1, indistinguishable from a failed
// deploy, so a script could not tell a typo from an SSH failure.
func TestRunDeployUsageErrorExitsTwoOnStderr(t *testing.T) {
	var code int
	var errOut string
	clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() {
			code = clitest.CovT_capExit(t, func() { RunDeploy([]string{"--zzz-unknown-flag=1"}) })
		})
	})
	if code != 2 {
		t.Fatalf("deploy --zzz exited %d, want 2", code)
	}
	if !strings.Contains(errOut, "zzz-unknown-flag") {
		t.Fatalf("deploy --zzz diagnostic missing on stderr:\n%s", errOut)
	}
}

// Bare `gofastr deploy` names no server: a missing argument is a usage
// error, exit 2.
func TestRunDeployMissingServerExitsTwo(t *testing.T) {
	var code int
	var errOut string
	clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() {
			code = clitest.CovT_capExit(t, func() { RunDeploy(nil) })
		})
	})
	if code != 2 {
		t.Fatalf("bare deploy exited %d, want 2", code)
	}
	if !strings.Contains(errOut, "name a server") {
		t.Fatalf("bare deploy diagnostic missing on stderr:\n%s", errOut)
	}
}
