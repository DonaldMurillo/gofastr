package pages

import (
	"example.com/shape-zoo/kitx"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func footer() render.HTML { return ui.SiteFooter(kitx.FooterConfig{}) } // zoo:hit siteheader
