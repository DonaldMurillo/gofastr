package runtime

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The runtime's own words: the fallback notice, failed navigations and
// requests, a server action's default success toast, the panel stack,
// a deferred region that could not load, the embed frame and the form
// helpers' counters. frag/*.js and
// src/*.js say each one through say('runtime.<code>', 'English', params).
var (
	sayNotifications     = i18n.Define("runtime.notifications", "Notifications")
	sayDismissNotice     = i18n.Define("runtime.dismiss_notification", "Dismiss notification")
	sayLoadFailedStatus  = i18n.Define("runtime.load_failed_status", "Could not load {{path}} ({{status}})")
	sayLoadFailed        = i18n.Define("runtime.load_failed", "Could not load {{path}}: check your connection")
	sayNeedsServer       = i18n.Define("runtime.needs_server", "Needs the Go server.")
	sayPanelFailed       = i18n.Define("runtime.panel_failed", "Could not open that panel.")
	sayPanelLimit        = i18n.Define("runtime.panel_limit", "{{n}} panels is the limit")
	sayPanelLimitHint    = i18n.Define("runtime.panel_limit_hint", "Open this one as a page, or close a panel first.")
	sayPartFailed        = i18n.Define("runtime.part_failed", "This section could not load.")
	saySubmitFailed      = i18n.Define("runtime.submit_failed", "Could not complete that submission.")
	sayRequestFailed     = i18n.Define("runtime.request_failed", "Request failed")
	sayRequestFailedCode = i18n.Define("runtime.request_failed_status", "Request failed ({{status}})")
	sayNetworkError      = i18n.Define("runtime.network_error", "Network error: please try again")
	sayDone              = i18n.Define("runtime.done", "Done")
	sayEmbedFailed       = i18n.Define("runtime.embed_failed", "This panel could not load. Error: {{code}}.")
	sayEmbedNoLinks      = i18n.Define("runtime.embed_no_links", "This panel cannot open links.")
	sayEmbedNoForms      = i18n.Define("runtime.embed_no_forms", "This panel cannot submit this form.")
	sayCharCount         = i18n.Define("runtime.charcount", "{{n}} chars")
	sayElapsedMS         = i18n.Define("runtime.elapsed_ms", "{{n}}ms")
	sayElapsedS          = i18n.Define("runtime.elapsed_s", "{{n}}s")
)

func init() {
	RegisterStrings(sayNotifications, sayDismissNotice, sayLoadFailedStatus, sayLoadFailed,
		sayNeedsServer, sayPanelFailed, sayPanelLimit, sayPanelLimitHint, sayPartFailed, saySubmitFailed,
		sayRequestFailed, sayRequestFailedCode, sayNetworkError, sayDone, sayEmbedFailed,
		sayEmbedNoLinks, sayEmbedNoForms, sayCharCount, sayElapsedMS, sayElapsedS)
}
