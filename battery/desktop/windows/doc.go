// Package windows is the Windows arm of the desktop host. It answers
// the unsupported shell on every GOOS today: the plan (docs/
// desktop-plan.md, phase 5) is WebView2 in-process through
// syscall.NewLazyDLL / syscall.SyscallN / syscall.NewCallback (no cgo,
// no linknames: the standard library already has everything) with DWM
// Mica/Acrylic for the window materials, reusing the contract's menu
// plan with Win32 mask bits. Until that lands, New keeps the package
// compiling on every GOOS so a host importing battery/desktop/native
// cross-compiles without build tags of its own.
package windows

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell. It becomes the real WebView2 shell
// when the Windows phase lands.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
