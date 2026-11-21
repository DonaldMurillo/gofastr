//go:build darwin || linux

package procalive

import (
	"os/exec"
	"testing"
	"time"
)

// An exited child its parent has not reaped is a zombie: it still
// answers kill(pid, 0), but it is not running. battery/service keeps a
// daemon's pid in a file, and an unreaped daemon must read as gone.
func TestZombieIsNotAlive(t *testing.T) {
	child := exec.Command("sleep", "0")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Wait() }) // reap; the test never may leave one
	pid := child.Process.Pid
	dead := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if dead = zombie(pid); dead {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !dead {
		t.Fatalf("pid %d never became a zombie", pid)
	}
	if Alive(pid) {
		t.Fatal("a zombie answered as alive")
	}
	id := Identify(pid)
	if id.Start == 0 {
		t.Fatal("no start value for the zombie: the identity check is vacuous")
	}
	if id.Alive() {
		t.Fatal("a zombie's matching start value read as alive")
	}
}
