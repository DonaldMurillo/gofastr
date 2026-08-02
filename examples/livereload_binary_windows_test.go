//go:build windows

package main

import "path/filepath"

func livereloadBinaryPath(path string) string {
	if filepath.Ext(path) == "" {
		return path + ".exe"
	}
	return path
}
