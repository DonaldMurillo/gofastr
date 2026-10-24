package setup

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonSetupTokenInvalid  = i18n.Define("setup.token_invalid", "forbidden: invalid or expired setup token. Check the server startup log for the setup URL.")
	reasonSetupTokenRequired = i18n.Define("setup.token_required", "forbidden: setup token required. Check the server startup log for the setup URL.")
	reasonSetupTokenUsed     = i18n.Define("setup.token_used", "forbidden: setup token already used or expired. Continue in your original browser session, or restart the app to mint a fresh token.")
)
