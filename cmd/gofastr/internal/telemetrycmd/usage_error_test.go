package telemetrycmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A mistake on the command line is a usage error: the diagnostic goes to
// stderr and the exit status is 2, the status every gofastr command
// shares for bad usage. It used to exit 1, indistinguishable from a
// failed read of the telemetry files.
func TestRunTelemetryUsageErrorExitsTwoOnStderr(t *testing.T) {
	var code int
	var errOut string
	clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() {
			code = clitest.CovT_capExit(t, func() { RunTelemetry([]string{"--zzz-unknown-flag=1"}) })
		})
	})
	if code != 2 {
		t.Fatalf("telemetry --zzz exited %d, want 2", code)
	}
	if !strings.Contains(errOut, "zzz-unknown-flag") {
		t.Fatalf("telemetry --zzz diagnostic missing on stderr:\n%s", errOut)
	}
}

// Bare `gofastr telemetry` names no subcommand: a missing argument is a
// usage error too, so exit 2 with the usage on stderr.
func TestRunTelemetryMissingSubcommandExitsTwo(t *testing.T) {
	var code int
	var errOut string
	clitest.CovT_capStdout(t, func() {
		errOut = clitest.CovT_capStderr(t, func() {
			code = clitest.CovT_capExit(t, func() { RunTelemetry(nil) })
		})
	})
	if code != 2 {
		t.Fatalf("bare telemetry exited %d, want 2", code)
	}
	if !strings.Contains(errOut, "name a subcommand") {
		t.Fatalf("bare telemetry diagnostic missing on stderr:\n%s", errOut)
	}
}
