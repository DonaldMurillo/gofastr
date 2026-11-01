//go:build unix

package devcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Interrupting a build killed `go` alone. The compiler, the linker and
// cgo's C toolchain run as its children and kept running, holding the
// output pipe and the CPU. The build runs in a group of its own, and
// the interrupt kills the group.
func TestInterruptKillsBuildChildren(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	// A stand-in for go build: a child that outlives a kill of its parent.
	build := exec.Command("sh", "-c", `sleep 30 & echo $! > "$1"; wait`, "sh", pidFile)
	interrupt := make(chan struct{})
	go func() {
		for range 200 {
			if b, _ := os.ReadFile(pidFile); len(b) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(interrupt)
	}()
	if err := RunBuildWithHeartbeat(build, "test", interrupt); !errors.Is(err, ErrBuildInterrupted) {
		t.Fatalf("want ErrBuildInterrupted, got %v", err)
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	// Orphaned, the child is reaped by init once it dies.
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the build's child (pid %d) outlived the interrupt", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
