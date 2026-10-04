// Package kitx renames kit types for the app. An alias is a use of the
// type it names; a shapes note reads through it to the kit type, so the
// Resp alias itself is no hit for the Finish signature change.
package kitx

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type Resp = middleware.IdempotentResponse

type FooterConfig = ui.SiteFooterConfig // zoo:hit siteheader
