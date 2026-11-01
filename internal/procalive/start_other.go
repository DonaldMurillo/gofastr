//go:build !linux && !darwin && !windows

package procalive

// startTime has no reader on this OS: an Identity here is its pid alone.
func startTime(int) uint64 { return 0 }
