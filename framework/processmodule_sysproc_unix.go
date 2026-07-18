//go:build !windows

package framework

import (
	"os/exec"
	"syscall"
)

// setChildProcessGroup puts the spawned module child in its own process
// group (Setpgid:true) on Unix so SIGTERM/SIGINT to the host does not
// propagate to it before the supervisor's drain sequence runs, and so the
// supervisor can signal the whole group (kill -(-pgid)) to clean up a
// grandchild the module may have forked.
//
// The design (§6 baseline hygiene) requires an own process group for BOTH
// runners; SandboxRunner will tighten it further via per-OS backends. The
// build-tag split is !windows vs windows (mirroring
// framework/harness/hook/procgroup_{unix,windows}.go) — Darwin is Unix and
// has Setpgid, so it stays on this side.
func setChildProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// processPgid returns the process group id for a child spawned with
// Setpgid:true (equal to the child's own pid under the no-Pgid convention).
// It is read here rather than cached in the runner so the build-tag split
// keeps the Unix vs Windows divergence in one file.
func processPgid(pid int) int { return pid }

// signalProcessGroup sends sig to every process in pgid's group. It is the
// teardown primitive the supervisor uses after stdin.Close + drain deadline
// (design §4.6 lift list). Negating pgid targets the whole group.
func signalProcessGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}
