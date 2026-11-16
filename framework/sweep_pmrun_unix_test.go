//go:build !windows

package framework

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

// Kill on a reaped child sends nothing to the group id it recorded, which
// may lead another process's group by then.
func TestSweepPMRunKillAfterReapSignalsNothing(t *testing.T) {
	gone := exec.Command("/bin/sh", "-c", "exit 0")
	setChildProcessGroup(gone)
	if err := gone.Start(); err != nil {
		t.Fatal(err)
	}
	c := &spawnedChild{cmd: gone, pgid: childPgid(gone)}
	_ = c.Wait()

	other := exec.Command("/bin/sleep", "30")
	setChildProcessGroup(other)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	c.pgid = other.Process.Pid
	_ = c.Kill()
	if err := syscall.Kill(other.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := other.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("other group's leader: wait err %v", err)
	}
	ws := exit.Sys().(syscall.WaitStatus)
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("other group's leader ended by %v, want only the test's SIGTERM: Kill after reap signalled a group the child no longer owns", ws)
	}
}
