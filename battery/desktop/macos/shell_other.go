//go:build !(darwin && (arm64 || amd64))

package macos

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

// New answers the unsupported shell off darwin/arm64 and darwin/amd64.
func New() desktop.Shell {
	return desktop.NewUnsupportedShell()
}
