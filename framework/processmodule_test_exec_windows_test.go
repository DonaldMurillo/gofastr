//go:build windows

package framework

import "path/filepath"

func testExecutablePath(path string) string {
	if filepath.Ext(path) == "" {
		return path + ".exe"
	}
	return path
}
