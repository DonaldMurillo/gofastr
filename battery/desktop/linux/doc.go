// Package linux is the Linux arm of the desktop host. It answers the
// unsupported shell on every GOOS today: the plan (docs/desktop-plan.md,
// phase 6) is WebKitGTK in-process through dlopen on a pure-Go fake-cgo
// layer (battery/desktop/internal/fakecgo plus internal/gtk), reusing
// the contract's menu plan. Until that lands, New keeps the package
// compiling on every GOOS so a host importing battery/desktop/native
// cross-compiles without build tags of its own.
package linux

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell. It becomes the real WebKitGTK shell
// when the Linux phase lands.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
