//go:build unix

package devloop

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ownProcessGroup starts cmd in its own process group so killGroup can
// reap what it leaves behind: a `gofastr dev` the agent backgrounded and
// never stopped would otherwise hold a port into the next trial. cmd
// must come from exec.CommandContext: Start rejects a Cancel otherwise.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// detach starts cmd in a new session, the way `setsid` does.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// reapUnder kills every process whose working directory is dir or below
// it. Matching on the working directory, checked at kill time, reaches a
// server that left the agent's process group (setsid, a new session)
// without trusting a PID recorded earlier that may since have been
// reused. It runs twice: a dev server killed mid-rebuild can have just
// started a child.
func reapUnder(dir string) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return
	}
	self := os.Getpid()
	for range 2 {
		for _, pid := range pidsUnder(root) {
			if pid != self {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pidsUnder lists processes whose working directory is root or below
// it: from /proc on Linux, from lsof elsewhere.
func pidsUnder(root string) []int {
	under := func(cwd string) bool {
		return cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator))
	}
	var pids []int
	if entries, err := os.ReadDir("/proc"); err == nil && len(entries) > 0 {
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			if cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd")); err == nil && under(cwd) {
				pids = append(pids, pid)
			}
		}
		return pids
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "lsof", "-n", "-d", "cwd", "-F", "pn")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		// lsof missing or killed: no listing to read. An exit status
		// alone is fine, since lsof exits 1 when some processes are
		// unreadable and the rest of its output is still valid.
		return nil
	}
	pid := 0
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0 && under(line[1:]):
			pids = append(pids, pid)
		}
	}
	return pids
}

const supported = true
