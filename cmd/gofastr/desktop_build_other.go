//go:build !windows

package main

import "fmt"

func buildWindowsDesktop(_ desktopBuildFlags, _ string) error {
	return fmt.Errorf("Windows desktop packaging is only available when gofastr runs on Windows")
}
