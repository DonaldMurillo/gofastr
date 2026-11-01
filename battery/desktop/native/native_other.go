//go:build (!darwin && !windows && !linux) || gofastr_nodesktop

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// Shell is the unsupported shell on a platform with no native one, and
// on every platform in a build tagged gofastr_nodesktop: that build
// links no platform package (no WebView, no GTK loader, no fakecgo), so
// a CGO_ENABLED=0 Linux binary that never opens a window stays static
// and starts on Alpine or scratch. Run then fails with the unsupported
// shell's error instead of opening a window.
func Shell() desktop.Shell { return desktop.NewUnsupportedShell() }
