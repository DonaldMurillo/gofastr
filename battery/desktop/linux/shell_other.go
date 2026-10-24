//go:build !linux || (!amd64 && !arm64)

package linux

import "github.com/DonaldMurillo/gofastr/battery/desktop"

// New returns an unsupported shell on platforms without this native layer.
func New() desktop.Shell { return desktop.NewUnsupportedShell() }
