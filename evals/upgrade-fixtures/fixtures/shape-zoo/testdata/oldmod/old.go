// Package old sits under testdata: the go tool ignores it, so must the scan.
package old

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func Old() render.HTML { return ui.SiteHeader(ui.SiteHeaderConfig{}) }
