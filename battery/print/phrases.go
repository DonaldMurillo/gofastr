package print

import "github.com/DonaldMurillo/gofastr/core/i18n"

var (
	reasonPDFBusy          = i18n.Define("print.busy", "PDF rendering is busy; retry shortly")
	reasonPDFFailed        = i18n.Define("print.render_failed", "PDF rendering failed")
	reasonPDFNotConfigured = i18n.Define("print.not_configured", "PDF rendering is not configured for this app")
)

var printLabel = i18n.Define("print.link", "Print")
