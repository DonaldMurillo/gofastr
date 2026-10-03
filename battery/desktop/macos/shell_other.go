//go:build !(darwin && arm64)

package macos

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell off darwin/arm64 (the AppKit +
// WKWebView layer is arm64-only today; macOS amd64 is its own phase).
// The package still compiles on every GOOS/GOARCH so a host importing
// battery/desktop/native cross-compiles without build tags of its own.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
