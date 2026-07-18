//go:build windows

package framework

import (
	"os/exec"
	"syscall"
)

// setChildProcessGroup is the Windows arm of the build-tagged process-group
// split. Windows has no Setpgid; CreateProcess under a Job Object is the
// equivalent kill-tree primitive and lives in SandboxRunner (later wave).
// For now, baseline hygiene sets CREATE_NEW_PROCESS_GROUP via CreationFlags
// so the child does not receive console-group CTRL events meant for the host.
//
// SandboxRunner (design §6) tightens this with a Job Object (P6: kill-tree).
// The TrustedProcessRunner's contract on Windows is crash isolation only, as
// the design states.
func setChildProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
}

// processPgid is the Windows no-op arm. Windows has no process groups in
// the Unix sense; teardown uses the recorded *os.Process handle and (in a
// later wave) a Job Object. Returning 0 lets the runner's Kill path fall
// through to (*os.Process).Kill.
func processPgid(_ int) int { return 0 }

// signalProcessGroup is the no-op Windows arm. Process-tree kill on Windows
// goes through the recorded *os.Process handle (supervisor calls
// (*os.Process).Kill on the child), and a future SandboxRunner will use a
// Job Object to clean up grandchildren. The function exists so the
// supervisor's teardown code is cross-platform without #ifdef at every site.
func signalProcessGroup(_ int, _ syscall.Signal) error {
	return nil
}
