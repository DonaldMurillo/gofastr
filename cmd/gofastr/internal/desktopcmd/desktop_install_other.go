//go:build !linux

package desktopcmd

import "fmt"

func installLinuxBundle(string) error {
	return fmt.Errorf("desktop install is supported on Linux only")
}
func uninstallLinuxBundle(string) error {
	return fmt.Errorf("desktop uninstall is supported on Linux only")
}
