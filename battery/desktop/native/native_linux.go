//go:build linux && !gofastr_nodesktop

package native

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/linux"
)

func Shell() desktop.Shell { return linux.New() }
