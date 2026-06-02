//go:build windows

package main

import "os"

// peakRSSFromState is unavailable on Windows: syscall.Rusage has no
// Maxrss field there. Build peak-RSS is reported as 0 (the bench tool's
// other metrics — binary size, build wall, runtime RAM — still apply).
func peakRSSFromState(_ *os.ProcessState) int64 { return 0 }
