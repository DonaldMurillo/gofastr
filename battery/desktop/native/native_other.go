//go:build (!darwin && !windows && !linux) || gofastr_nodesktop

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Shell returns the unsupported shell.
func Shell() desktop.Shell { return desktop.NewUnsupportedShell() }
