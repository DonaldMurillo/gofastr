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
	for deadline := time.Now().Add(stopGrace); len(cellPIDs(dir)) > 0 && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
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

// lockAbandoned reports whether the lock at path can be removed: no
// process has its working directory in cell and none has path open. Git
// closes index.lock before running commit hooks and renames it afterwards,
// so a closed lock alone does not mean its writer is gone; a git working
// on the cell runs with its working directory there.
func lockAbandoned(cell, path string) bool {
	if real, err := filepath.EvalSymlinks(cell); err == nil {
		cell = real
	}
	return len(cellPIDs(filepath.Clean(cell))) == 0 && !fileHeldOpen(path)
}

func fileHeldOpen(path string) bool {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", p.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join("/proc", p.Name(), "fd", fd.Name())); err == nil && target == path {
				return true
			}
		}
	}
	return false
}
