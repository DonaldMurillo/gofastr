//go:build !windows

package framework

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Run unconfined, each probe body that can attempt its forbidden action in
// this process attempts it, succeeds, and reports BREACH: a body that
// passes without trying would let a backend that enforces nothing conform.
func TestSweepPMHostUnconfinedProbeBodiesBreach(t *testing.T) {
	t.Run("P1", func(t *testing.T) {
		t.Setenv("GOFASTR_PROBE_HOST_UID", hostUIDString())
		t.Setenv("GOFASTR_PROBE_HOST_PID", fmt.Sprintf("%d", os.Getpid()))
		wantBreach(t, runProbeBodyLine(t, ProbeDistinctPrincipal))
	})
	t.Run("P2", func(t *testing.T) {
		t.Setenv("GOFASTR_SWEEP_CANARY", "planted")
		t.Setenv("GOFASTR_PROBE_CANARY_NAME", "GOFASTR_SWEEP_CANARY")
		wantBreach(t, runProbeBodyLine(t, ProbeNoInheritedSecret))
	})
	t.Run("P5", func(t *testing.T) {
		t.Setenv("GOFASTR_PROBE_SCRATCH", t.TempDir())
		t.Setenv("GOFASTR_PROBE_HOME", "")
		wantBreach(t, runProbeBodyLine(t, ProbeFilesystemConfinement))
	})
}

func wantBreach(t *testing.T, line string) {
	t.Helper()
	if !strings.HasPrefix(line, probeOutBreach+" ") {
		t.Fatalf("unconfined probe body printed %q, want a %s line", line, probeOutBreach)
	}
}
