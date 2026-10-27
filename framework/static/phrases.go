package static

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The notice a static export carries on every page. The export renders
// once with the build's context, so it speaks the language that context
// names (English with none).
var (
	pageStaticTitle = i18n.Define("static.preview_title", "Static preview")
	pageStaticBody  = i18n.Define("static.preview_body", "This is a read-only export. Run the app locally for full interactivity: demos and server-driven islands need the Go server.")
)

// The page an exported redirect route leaves at its old path.
var (
	pageRedirectTitle = i18n.Define("static.redirect_title", "Page moved")
	pageRedirectLink  = i18n.Define("static.redirect_link", "Continue to the new page")
)
