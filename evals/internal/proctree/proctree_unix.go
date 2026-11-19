//go:build unix

package proctree

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

// KillUnder ends processes working in dir or below, allowing grace for SIGTERM.
func KillUnder(dir string, grace time.Duration) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return
	}
	root = filepath.Clean(root)
	for range 2 {
		pids := pidsUnder(root)
		if len(pids) == 0 {
			return
		}
		signalPids(pids, syscall.SIGTERM)
		if survivors := waitPidsGone(root, grace); len(survivors) > 0 {
			signalPids(survivors, syscall.SIGKILL)
			waitPidsGone(root, grace)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func signalPids(pids []int, signal syscall.Signal) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, signal)
	}
}

func waitPidsGone(root string, grace time.Duration) []int {
	deadline := time.Now().Add(grace)
	empty := false
	for {
		pids := pidsUnder(root)
		if !time.Now().Before(deadline) || (empty && len(pids) == 0) {
			return pids
		}
		// A scan can miss a fork that races with the parent's exit.
		empty = len(pids) == 0
		time.Sleep(50 * time.Millisecond)
	}
}

// pidsUnder lists processes whose working directory is root or below
// it: from /proc on Linux, from lsof elsewhere.
func pidsUnder(root string) []int {
	under := func(cwd string) bool {
		return cwd == root || strings.HasPrefix(cwd, root+string(filepath.Separator))
	}
	self := os.Getpid()
	var pids []int
	if entries, err := os.ReadDir("/proc"); err == nil && len(entries) > 0 {
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil || pid == self {
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
		case strings.HasPrefix(line, "n") && pid > 0 && pid != self && under(line[1:]):
			pids = append(pids, pid)
		}
	}
	return pids
}
