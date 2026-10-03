// Package kitx renames kit types for the app. An alias is a use of the
// type it names.
package kitx

import (
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type Resp = middleware.IdempotentResponse // zoo:hit finish

type FooterConfig = ui.SiteFooterConfig // zoo:hit siteheader
