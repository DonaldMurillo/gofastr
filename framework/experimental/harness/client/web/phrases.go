package web

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonEmptyText       = i18n.Define("harness.empty_text", "empty text")
	reasonNonLoopbackHost = i18n.Define("harness.non_loopback_host", "forbidden: non-loopback Host")
	reasonSendFailed      = i18n.Define("harness.send_failed", "could not send: {{detail}}")
)
