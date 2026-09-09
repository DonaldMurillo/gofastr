// Package linux is the Linux arm of the desktop host. It answers the
// unsupported shell on every GOOS today: the plan (docs/desktop-plan.md,
// phase 6) is WebKitGTK in-process through dlopen on a pure-Go
// fake-cgo layer (battery/desktop/internal/fakecgo plus internal/gtk),
// reusing the contract's menu plan. Until that lands, New keeps the
// package compiling on every GOOS so a host importing
// battery/desktop/native cross-compiles without build tags of its
// own.
//
// The phase 13 chrome fields (docs/desktop-sections/13-chrome.md)
// will be fulfilled this way when the Linux phase lands: blur belongs
// to the compositor, so every WindowStyle.Material answers the
// unsupported default and the page falls back to opaque (the plan's
// commitment); ChromeUnified is GTK CSD with a page-painted headerbar;
// the focus callbacks ride the GTK window's focus events; Reduce
// Transparency has no OS-wide setting, so Appearance answers false
// and no reduce_transparency event fires. A compositor that blurs
// (a blurred shell region behind an RGBA visual with
// webkit_web_view_set_background_color) is the one opening a Material
// fulfilment would have, and it stays out until someone proves it.
package linux

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell. It becomes the real WebKitGTK shell
// when the Linux phase lands.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
