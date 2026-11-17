package static

import "github.com/DonaldMurillo/gofastr/core/i18n"

var (
	pageStaticTitle = i18n.Define("static.preview_title", "Static preview")
	pageStaticBody  = i18n.Define("static.preview_body", "This is a read-only export. Run the app locally for full interactivity: demos and server-driven islands need the Go server.")
)

var (
	pageRedirectTitle = i18n.Define("static.redirect_title", "Page moved")
	pageRedirectLink  = i18n.Define("static.redirect_link", "Continue to the new page")
)
