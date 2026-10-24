package island

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonSessionMissing = i18n.Define("island.session_missing", "missing session query parameter")
	reasonSessionTooLong = i18n.Define("island.session_too_long", "session query parameter too long")
)
