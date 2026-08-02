//go:build !windows

package framework

func testExecutablePath(path string) string { return path }
