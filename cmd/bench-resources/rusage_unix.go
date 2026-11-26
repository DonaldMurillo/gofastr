//go:build !windows

package main

import (
	"os"
	"runtime"
	"syscall"
)

func peakRSSFromState(state *os.ProcessState) int64 {
	if state == nil {
		return 0
	}
	if ru, ok := state.SysUsage().(*syscall.Rusage); ok {
		return normaliseRSS(int64(ru.Maxrss)) // int32 on 32-bit Linux
	}
	return 0
}

func normaliseRSS(raw int64) int64 {
	if raw <= 0 {
		return 0
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return raw
	}
	return raw * 1024
}
