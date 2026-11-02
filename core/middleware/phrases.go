package middleware

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonRateLimited       = i18n.Define("error.rate_limited", "rate limit exceeded")
	reasonCSRFBadSignature  = i18n.Define("csrf.bad_signature", "csrf: invalid token signature")
	reasonCSRFFormTooLarge  = i18n.Define("csrf.form_too_large", "csrf: form body too large")
	reasonCSRFMissingCookie = i18n.Define("csrf.missing_cookie", "csrf: missing cookie")
	reasonCSRFTokenMismatch = i18n.Define("csrf.token_mismatch", "csrf: token mismatch")
	reasonIdemInFlight      = i18n.Define("idempotency.in_flight", "concurrent request for this Idempotency-Key")
	reasonIdemKeyMissing    = i18n.Define("idempotency.key_missing", "missing Idempotency-Key header")
	reasonIdemKeyReused     = i18n.Define("idempotency.key_reused", "Idempotency-Key reused with different request")
	reasonIdemKeyTooLong    = i18n.Define("idempotency.key_too_long", "Idempotency-Key too long")
	reasonIdemKeyInvalid    = i18n.Define("idempotency.key_invalid", "Idempotency-Key must be printable ASCII")
	reasonIdemStoreDown     = i18n.Define("idempotency.store_unavailable", "idempotency store unavailable")
)
