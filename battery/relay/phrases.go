package relay

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonUpstreamTimeout     = i18n.Define("relay.upstream_timeout", "upstream timeout")
	reasonUpstreamUnavailable = i18n.Define("relay.upstream_unavailable", "upstream unavailable")
	reasonSwitchRefused       = i18n.Define("relay.switch_refused", "upstream protocol switch refused")
)
