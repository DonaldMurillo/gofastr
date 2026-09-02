//go:build !windows

package framework

// Audit-slice proof tests (issue #136) for the sandbox conformance-probe
// runner (processmodule_probe.go). RED against current code on purpose: each
// encodes a contract the file itself documents, so the failure output IS the
// finding. They turn green when the corresponding gap is fixed.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestAuditParseProbeOutputKilledChildContract pins the stdout-protocol
// contract written above the probeOut* constants (processmodule_probe.go,
// "Probe-child stdout protocol"):
//
//	"A child that exits without printing (killed by the sandbox mid-attempt,
//	 or crashed) is treated as UNREACHABLE for P6 (the limit may have fired)
//	 and FAIL for everything else (the sandbox did not deny the action; it
//	 killed the child, which is not the same as a clean denial, surface it)."
//
// parseProbeOutput returns Unreachable for EVERY probe on the no-output
// path, so a sandbox that kills the child mid-attempt is reported as
// "unreachable" rather than "failed" for P1–P5/P7. Both block Conforms()
// (fail-closed either way), but the operator-facing report mislabels a kill
// as "could not run" — the exact distinction the comment says to surface.
func TestAuditParseProbeOutputKilledChildContract(t *testing.T) {
	// Pin: P6 no-output stays Unreachable per contract.
	if got := parseProbeOutput(ProbeResourceLimits, "", false, ""); got.Status != ProbeStatusUnreachable {
		t.Errorf("P6 no-output = %v, want Unreachable (contract)", got.Status)
	}
	// FINDING: non-P6 no-output must be FAIL per the documented contract.
	for _, p := range []ProbeID{ProbeDistinctPrincipal, ProbeNoInheritedSecret, ProbeNoInheritedFD, ProbeNoNetworkEgress, ProbeFilesystemConfinement, ProbeNoPrivReEscalation} {
		got := parseProbeOutput(p, "", false, "")
		if got.Status != ProbeStatusFail {
			t.Errorf("FINDING: %s killed-without-output = %v (%q), want Fail per the stdout-protocol contract — a kill is not a clean denial and must be surfaced as such", p, got.Status, got.Detail)
		}
	}
}

// TestAuditParseProbeOutputStderrShadowsResult: runOneProbe wires the child's
// stdout AND stderr into ONE bytes.Buffer and parseProbeOutput trusts the
// FIRST line. A sandbox wrapper that prints anything to stderr before the
// child's result line (sandbox-exec prints warnings; the wrapper itself owns
// the pipe until it execs) flips a clean PASS into
// "unrecognized output" → Unreachable. Observed live on darwin 25.5.0
// (2026-09-01): every probe's Detail was
// `unrecognized output: sandbox-exec: execvp() ... Operation not permitted`,
// i.e. the wrapper's stderr became the parsed line. The parse must find the
// child's sentinel line, not let an interleaved wrapper line veto it.
func TestAuditParseProbeOutputStderrShadowsResult(t *testing.T) {
	mixed := "sandbox-exec: warning: compilation options changed\n" + probeOutPass + " uid isolated"
	got := parseProbeOutput(ProbeDistinctPrincipal, mixed, false, mixed)
	if got.Status != ProbeStatusPass {
		t.Errorf("FINDING: wrapper stderr line shadowed the child's %q line → %v (%q); want Pass — the parser must locate the child's sentinel, not take the first line of the shared stdout+stderr buffer", probeOutPass, got.Status, got.Detail)
	}
}

// escapeBackend is a hostile SandboxBackend for the runner (not backend)
// test: its Wrap replaces the probe child with a shell that (a) spawns a
// grandchild which calls setsid() — escaping the process group — and holds
// the inherited stdout pipe open, writing its pid to gpid in the cwd, then
// (b) execs a long sleep as the direct child. Nothing here depends on a real
// sandbox; the point is what runOneProbe does when a descendant outside the
// killed process group holds the output pipe.
type escapeBackend struct {
	grandchild string // shell command for the escaping grandchild
}

func (e *escapeBackend) Name() string              { return "escape" }
func (e *escapeBackend) Available() bool           { return true }
func (e *escapeBackend) MissingReason() string     { return "" }
func (e *escapeBackend) DeclaredProbes() []ProbeID { return allProbes }
func (e *escapeBackend) Wrap(cmd *exec.Cmd, _ SandboxOpts) error {
	cmd.Path = "/bin/sh"
	cmd.Args = []string{"/bin/sh", "-c", e.grandchild + " & exec /bin/sleep 600"}
	return nil
}

// TestAuditProbeRunnerWaitUnboundedPastTimeout proves the probe wall budget
// is not actually a bound: runOneProbe never sets cmd.WaitDelay, so after
// probeTimeout fires and killProcessTree kills the child's process group,
// the unconditional `<-waitErr` blocks on cmd.Wait, which blocks on the
// stdout/stderr copier until EVERY holder of the pipe's write end exits. A
// descendant that escaped the group (setsid) holds it indefinitely — the
// exact shape codegen/extension_command.go fixed with WaitDelay
// ("Wait blocks forever on a pipe nobody will ever close") and that
// evals/*/process_tree_*.go also guards. The probeTimeout comment claims
// "exceeding it means the probe cannot complete on this host", but the
// implementation does not enforce completion, it hangs.
//
// Not reachable through today's probe bodies (they never setsid), so this is
// a latent-hang finding about the runner, proven with a hostile Wrap: the
// Wrap seam is exactly where arbitrary backend implementations plug in.
func TestAuditProbeRunnerWaitUnboundedPastTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process-group + setsid probe")
	}
	// Need a way to start a session-escaping process: python3 (os.setsid)
	// or the setsid(1) utility.
	var grandchild string
	if py, err := exec.LookPath("python3"); err == nil {
		grandchild = fmt.Sprintf("%s -c %q", py, "import os;os.setsid();open('gpid','w').write(str(os.getpid()));import time;time.sleep(35)")
	} else if ss, err := exec.LookPath("setsid"); err == nil {
		grandchild = fmt.Sprintf("%s /bin/sh -c %q", ss, "echo $$ > gpid; exec /bin/sleep 35")
	} else {
		t.Skip("neither python3 nor setsid available to escape the process group")
	}

	scratch := t.TempDir()
	backend := &escapeBackend{grandchild: grandchild}

	started := time.Now()
	done := make(chan struct{})
	go func() {
		_ = runOneProbe(context.Background(), backend, ProbeNoPrivReEscalation, scratch)
		close(done)
	}()

	budget := time.After(probeTimeout + 8*time.Second)
	select {
	case <-done:
		// Returned inside the bound: WaitDelay-like bounding exists (fixed).
	case <-budget:
		t.Errorf("FINDING: runOneProbe is still blocked %s after start — %s past its %s wall budget. probeTimeout fired and killProcessTree killed the group, but cmd.Wait (no WaitDelay) is stuck on the output pipe held by the setsid grandchild; the budget the file documents is not enforced", time.Since(started).Round(time.Millisecond), time.Since(started)-probeTimeout, probeTimeout)
		// Clean up: kill the escaped grandchild so the leaked goroutine's
		// Wait unblocks and no orphan outlives the test binary.
		gpidPath := filepath.Join(scratch, ProbeNoPrivReEscalation.String(), "gpid")
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(gpidPath); err == nil {
				if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
					_ = syscall.Kill(pid, syscall.SIGKILL)
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
