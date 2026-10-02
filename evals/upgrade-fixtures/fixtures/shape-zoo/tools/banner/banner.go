// Package banner lives in a nested module the root ./... never loads.
package banner

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func Banner() render.HTML { return ui.SiteHeader(ui.SiteHeaderConfig{}) } // zoo:hit siteheader
