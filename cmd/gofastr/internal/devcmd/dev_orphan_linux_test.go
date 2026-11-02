//go:build linux

package devcmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const orphanHelperEnv = "GOFASTR_TEST_DEV_ORPHAN_HELPER"

// TestDevOrphanHelper is not a test: TestAppDiesWithKilledDev re-runs the
// test binary into it as a stand-in for gofastr dev. It starts an "app"
// the way buildAndServe does, prints its pid and waits to be killed.
func TestDevOrphanHelper(t *testing.T) {
	if os.Getenv(orphanHelperEnv) != "1" {
		t.Skip("helper process")
	}
	app := exec.Command("sleep", "60")
	TieToParent(app)
	if err := app.Start(); err != nil {
		fmt.Println("start:", err)
		os.Exit(1)
	}
	fmt.Println(app.Process.Pid)
	go func() { _ = app.Wait() }()
	time.Sleep(time.Minute)
}

// A gofastr dev killed with SIGKILL (a runner's timeout, the OOM killer)
// runs no cleanup. The app it started kept running, reparented to init
// and holding the dev address; the next gofastr dev then refused the
// port as "another gofastr dev serves it" when none did.
func TestAppDiesWithKilledDev(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	dev := exec.Command(os.Args[0], "-test.run=^TestDevOrphanHelper$")
	dev.Env = append(os.Environ(), orphanHelperEnv+"=1")
	stdout, err := dev.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Process.Kill(); _ = dev.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("helper printed no pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("helper: %q", line)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	_ = dev.Process.Kill()
	_ = dev.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processLive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("app pid %d still runs after its gofastr dev was SIGKILLed", pid)
}

// processLive reports a pid that exists and is not a zombie: an exited
// app reparented to a PID 1 that does not reap stays a zombie.
func processLive(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	// pid (comm) state ...: the state follows the last ')'.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || i+2 >= len(s) || s[i+2] != 'Z'
}
