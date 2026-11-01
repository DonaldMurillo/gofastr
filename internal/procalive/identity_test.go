//go:build darwin || linux

package procalive

import (
	"os"
	"os/exec"
	"testing"
)

// A recycled pid is a live process, but not the one that left: its
// start value differs. An Identity whose start does not match the live
// process holding its pid is gone.
func TestRecycledPIDIsNotAlive(t *testing.T) {
	if Identify(os.Getpid()).Start == 0 {
		t.Fatal("no start value for this process: the identity check is vacuous")
	}
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })

	id := Identify(child.Process.Pid)
	if !id.Alive() {
		t.Fatal("a running child read as gone")
	}
	// The same pid held by a process that started at another moment:
	// what the pid looks like after the kernel reuses it.
	other := Identity{PID: id.PID, Start: id.Start + 1}
	if other.Alive() {
		t.Fatal("a pid whose start value changed read as the same process")
	}
	if !(Identity{PID: id.PID}).Alive() {
		t.Fatal("an identity with no start value must fall back to the pid")
	}
	// A start value the OS will not report now (another user's process)
	// is not proof of a newcomer: the live pid stands. Reading it as gone
	// would let uninstall delete files under a running daemon.
	readStart = func(int) uint64 { return 0 }
	alive := id.Alive()
	readStart = startTime
	if !alive {
		t.Fatal("an unreadable start value made a live process read as gone")
	}

	_ = child.Process.Kill()
	_ = child.Wait()
	if id.Alive() {
		t.Fatal("a reaped child read as alive")
	}
}
