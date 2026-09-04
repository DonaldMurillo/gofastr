// Package b reproduces cmd/gofastr theme.go's dest local verbatim: a
// flag-parsed path whose default literal ("theme/theme.go") carries the
// public-extension evidence the last reassignment hides. Both writes
// stay quiet.
package b

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func osExit(int) {}

func runTheme(args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "init":
		runThemeInit(args[1:])
	default:
		fmt.Println("unknown")
		osExit(1)
	}
}

func runThemeInit(args []string) error {
	dest := "theme/theme.go"
	force := false
	for _, a := range args {
		switch {
		case a == "--force" || a == "-f":
			force = true
		case strings.HasPrefix(a, "--out=") || strings.HasPrefix(a, "-o="):
			dest = strings.SplitN(a, "=", 2)[1]
		default:
			osExit(1)
		}
	}
	if _, err := os.Stat(dest); err == nil && !force {
		osExit(1)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte("package theme\n"), 0o644)
}
