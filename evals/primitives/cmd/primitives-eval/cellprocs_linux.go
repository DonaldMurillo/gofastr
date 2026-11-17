package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// stopCellProcesses kills every process whose working directory is inside
// dir: background commands the agent started outlive the CLI, and a test
// run still going in the cell holds its git index while grading runs.
func stopCellProcesses(dir string) {
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
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
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}
