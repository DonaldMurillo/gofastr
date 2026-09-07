//go:build darwin

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/macos"
)

func Shell() desktop.Shell { return macos.New() }
