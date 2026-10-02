//go:build !windows

package pages

import (
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func header() render.HTML { return ui.SiteHeader(ui.SiteHeaderConfig{}) } // zoo:hit siteheader
