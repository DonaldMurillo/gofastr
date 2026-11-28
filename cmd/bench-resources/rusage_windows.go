//go:build windows

package main

import "os"

// Windows has no Rusage.Maxrss, so build peak RAM reads 0.

func peakRSSFromState(_ *os.ProcessState) int64 { return 0 }
