//go:build !windows || !amd64

package windows

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func New() desktop.Shell { return desktop.NewUnsupportedShell() }
