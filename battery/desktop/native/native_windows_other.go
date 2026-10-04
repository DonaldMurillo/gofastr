//go:build windows && !amd64

package native

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func Shell() desktop.Shell { return desktop.NewUnsupportedShell() }
