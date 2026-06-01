//go:build !windows

package main

import (
	"os"
	"runtime"
	"syscall"
)

// peakRSSFromState returns the peak resident set size (in bytes) of a
// finished subprocess, read from its Rusage. Returns 0 when unavailable.
func peakRSSFromState(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}
	if ru, ok := state.SysUsage().(*syscall.Rusage); ok {
		return normaliseRSS(ru.Maxrss)
	}
	return 0
}

// normaliseRSS converts the platform-specific Rusage.Maxrss to bytes.
//   - Darwin: bytes already.
//   - Linux:  kilobytes.
func normaliseRSS(raw int64) int64 {
	if raw <= 0 {
		return 0
	}
	if runtime.GOOS == "linux" {
		return raw * 1024
	}
	return raw // darwin, freebsd
}
