//go:build !(linux || darwin || freebsd)

package telemetry

func hostDisk(string) (used, total int64) { return 0, 0 }
