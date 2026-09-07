//go:build windows

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/windows"
)

func Shell() desktop.Shell { return windows.New() }
