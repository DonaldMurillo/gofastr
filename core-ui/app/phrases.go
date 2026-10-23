package app

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The words of the app-level fallback 404 page (its document title and
// its body) and the error page's document title.
var (
	pageNotFoundDocTitle = i18n.Define("app.not_found_doc_title", "Page not found")
	pageNotFoundTitle    = i18n.Define("app.not_found_title", "404: Page not found")
	pageNotFoundBody     = i18n.Define("app.not_found_body", "This page does not exist.")
	pageErrorDocTitle    = i18n.Define("app.error_doc_title", "Something went wrong")
)
