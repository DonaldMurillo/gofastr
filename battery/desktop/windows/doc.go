// Package windows is the Windows arm of the desktop host. It answers
// the unsupported shell on every GOOS today: the plan (docs/
// desktop-plan.md, phase 5) is WebView2 in-process through
// syscall.NewLazyDLL / syscall.SyscallN / syscall.NewCallback (no cgo,
// no linknames: the standard library already has everything) with DWM
// Mica/Acrylic for the window materials, reusing the contract's menu
// plan with Win32 mask bits. Until that lands, New keeps the package
// compiling on every GOOS so a host importing battery/desktop/native
// cross-compiles without build tags of its own.
//
// The phase 13 chrome fields (docs/desktop-sections/13-chrome.md)
// will be fulfilled this way when the Windows phase lands:
// WindowStyle.Material answers Mica for MaterialWindow
// (DWMSBT_MAINWINDOW) and Mica Alt (DWMSBT_TABBEDWINDOW) through
// DwmSetWindowAttribute(DWMWA_SYSTEMBACKDROP_TYPE) on build 22621+,
// with WebView2 DefaultBackgroundColor transparent for the page;
// there is no per-zone material, so MaterialSidebar degrades to a
// whole-window backdrop the page paints a translucent sidebar over,
// and MaterialGlass answers Desktop Acrylic (DWMSBT_TRANSIENTWINDOW),
// the nearest look. ChromeUnified is a page-drawn caption through
// WebView2 non-client regions (Caption, Minimize, Maximize, Close
// kinds, aligned with WM_NCHITTEST). The focus callbacks ride
// WM_ACTIVATE; Reduce Transparency has no OS setting on Windows, so
// Appearance answers false and no reduce_transparency event fires.
package windows

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell. It becomes the real WebView2 shell
// when the Windows phase lands.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
