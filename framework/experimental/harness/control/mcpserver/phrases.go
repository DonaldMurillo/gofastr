package mcpserver

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonNoAuthEncoder = i18n.Define("harness.no_auth_encoder", "mcp http handler: no auth encoder configured; refusing to serve unauthenticated")
)
