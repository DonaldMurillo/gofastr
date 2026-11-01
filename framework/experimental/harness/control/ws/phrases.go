package ws

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonInvalidSessionID = i18n.Define("harness.invalid_session_id", "invalid session id")
	reasonTokenMissing     = i18n.Define("harness.token_missing", "missing token")
	reasonTokenUnbound     = i18n.Define("harness.token_unbound", "token not bound to session")
	reasonBadLastEventID   = i18n.Define("harness.invalid_last_event_id", "invalid lastEventId")
)
