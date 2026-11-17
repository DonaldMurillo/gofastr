package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// stopGrace is how long a process the agent left in the cell gets to exit
// on SIGTERM, which lets git remove its own index.lock, before SIGKILL.
var stopGrace = 5 * time.Second

// stopCellProcesses ends every process whose working directory is inside
// dir: background commands the agent started outlive the CLI, and a test
// run still going in the cell holds its git index while grading runs.
func stopCellProcesses(dir string) {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	dir = filepath.Clean(dir)
	pids := cellPIDs(dir)
	if len(pids) == 0 {
		return
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	for deadline := time.Now().Add(stopGrace); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if len(cellPIDs(dir)) == 0 {
			return
		}
	}
	for _, pid := range cellPIDs(dir) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	for deadline := time.Now().Add(stopGrace); time.Now().Before(deadline) && len(cellPIDs(dir)) > 0; time.Sleep(50 * time.Millisecond) {
	}
	lock := filepath.Join(dir, ".git", "index.lock")
	if len(cellPIDs(dir)) == 0 && !heldOpen(lock) {
		// A writer killed outright cannot remove its lock; with nothing left
		// running in the cell and no process holding the file, it is abandoned.
		_ = os.Remove(lock)
	}
}

// heldOpen reports whether any process has path open; git keeps a lock file
// open for as long as it holds the lock, wherever it was started from.
func heldOpen(path string) bool {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", p.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(fdDir, fd.Name())); err == nil && target == path {
				return true
			}
		}
	}
	return false
}

func cellPIDs(dir string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		if err != nil {
			continue
		}
		if cwd == dir || strings.HasPrefix(cwd, dir+string(filepath.Separator)) {
			pids = append(pids, pid)
		}
	}
	return pids
}
