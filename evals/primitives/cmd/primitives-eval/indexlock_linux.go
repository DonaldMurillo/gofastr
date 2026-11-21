package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/evals/internal/proctree"
)

// lockAbandoned reports whether the lock at path can be removed: no git
// process is running, no process has its working directory in cell and
// none has path open. Git closes index.lock before running commit hooks
// and renames it afterwards, and a writer can work on the cell from
// outside it, so only a git that has exited has given the lock up.
func lockAbandoned(cell, path string) bool {
	if real, err := filepath.EvalSymlinks(cell); err == nil {
		cell = real
	}
	return !gitRunning() && len(proctree.PidsUnder(filepath.Clean(cell))) == 0 && !fileHeldOpen(path)
}

// gitRunning is a variable so tests can stub out other git processes on the host.
var gitRunning = func() bool {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", p.Name(), "comm"))
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(comm)), "git") {
			return true
		}
	}
	return false
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
