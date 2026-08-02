//go:build !windows

package evalrunner

func executableName(name string) string { return name }
