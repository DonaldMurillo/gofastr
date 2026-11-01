//go:build !windows

package framework

import (
	"bufio"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startGroupLeader starts sh in its own process group (as the runners do)
// running script, and returns it with the pid of the helper the script
// backgrounded (the script echoes $!).
func startGroupLeader(t *testing.T, script string) (*spawnedChild, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	setChildProcessGroup(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("read helper pid: %v", err)
	}
	helper, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("helper pid %q: %v", line, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(helper, syscall.SIGKILL) })
	return &spawnedChild{cmd: cmd, pgid: childPgid(cmd)}, helper
}

// alive reports whether pid still runs, waiting up to d for it to go (a
// killed helper is reaped by init a moment after the signal).
func alive(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return false
		}
		if time.Now().After(deadline) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A module that exits on its own (a drain that ends inside its deadline)
// takes the helpers in its process group with it. Only Kill signalled the
// group, and the drain path skips Kill when the child exits in time, so a
// helper survived beside the replacement generation.
func TestChildExitKillsItsGroup(t *testing.T) {
	c, helper := startGroupLeader(t, "sleep 30 >/dev/null 2>&1 & echo $!; exit 0")
	_ = c.Wait()
	if alive(helper, 2*time.Second) {
		t.Fatal("helper in the module's process group outlived the module's exit")
	}
}

// Once Wait has reaped the child, Kill signals no group: the recorded pgid
// may name an unrelated group by then.
func TestKillAfterReapSignalsNoGroup(t *testing.T) {
	c, _ := startGroupLeader(t, "sleep 30 >/dev/null 2>&1 & echo $!; exit 0")
	_ = c.Wait()
	other, otherHelper := startGroupLeader(t, "sleep 30 >/dev/null 2>&1 & echo $!; sleep 30")
	t.Cleanup(func() { _ = other.Kill(); _ = other.Wait() })
	c.pgid = other.pgid // the old number now leads someone else's group
	_ = c.Kill()
	if !alive(otherHelper, 500*time.Millisecond) {
		t.Fatal("Kill after reap signalled a process group the child no longer owns")
	}
}
