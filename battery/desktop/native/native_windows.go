//go:build windows && amd64

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/windows"
)

func Shell() desktop.Shell { return windows.New() }
