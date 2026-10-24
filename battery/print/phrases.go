package print

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonPDFBusy          = i18n.Define("print.busy", "PDF rendering is busy; retry shortly")
	reasonPDFFailed        = i18n.Define("print.render_failed", "PDF rendering failed")
	reasonPDFNotConfigured = i18n.Define("print.not_configured", "PDF rendering is not configured for this app")
)

// printLabel is PrintLink's default link text.
var printLabel = i18n.Define("print.link", "Print")
