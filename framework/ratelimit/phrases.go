package ratelimit

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonRateLimited = i18n.Define("error.rate_limited", "rate limit exceeded")
)
