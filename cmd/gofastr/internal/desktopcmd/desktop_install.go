package desktopcmd

import (
	"fmt"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"strings"
)

func runDesktopInstall(args []string, uninstall bool, targetOS string) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: gofastr desktop install --bundle=<extracted-directory>\n       gofastr desktop uninstall --id=<reverse.dns>\nLinux only. Installs for the current user; never selects a default URL handler.\nUninstall preserves application data. In-place replacement is refused.")
		return
	}
	if targetOS != "linux" {
		cli.Fail("desktop install/uninstall is supported on Linux only")
		cli.OsExit(1)
		return
	}
	flag := "--bundle"
	if uninstall {
		flag = "--id"
	}
	value, err := desktopInstallArgument(args, flag)
	if err == nil {
		if uninstall {
			err = uninstallLinuxBundle(value)
		} else {
			err = installLinuxBundle(value)
		}
	}
	if err != nil {
		cli.Fail("Desktop installation: %v", err)
		cli.OsExit(1)
	}
}
func desktopInstallArgument(args []string, name string) (string, error) {
	var value string
	switch {
	case len(args) == 1 && strings.HasPrefix(args[0], name+"="):
		value = strings.TrimPrefix(args[0], name+"=")
	case len(args) == 2 && args[0] == name && !strings.HasPrefix(args[1], "--"):
		value = args[1]
	}
	if value == "" {
		return "", fmt.Errorf("supply exactly %s=<value>; unknown, missing, and repeated flags are refused", name)
	}
	return value, nil
}
