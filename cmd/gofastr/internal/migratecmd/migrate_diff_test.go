package migratecmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The removed `gofastr migrate diff` printed its refusal and exited 0, so
// a deploy script running it carried on as if the schema had been
// reconciled (2026-10-31 bug hunt S64).
func TestRemovedMigrateDiffExitsNonZero(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunMigrate([]string{"diff"}) })
	})
	if code != 1 {
		t.Fatalf("`gofastr migrate diff` exit code = %d, want 1", code)
	}
}
