package sitesearch

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonBadLimit         = i18n.Define("sitesearch.bad_limit", "bad limit")
	reasonIndexUnavailable = i18n.Define("sitesearch.unavailable", "search index unavailable")
	reasonQueryTooLong     = i18n.Define("sitesearch.query_too_long", "query too long")
)
