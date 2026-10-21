package uihost

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals this package writes, under their catalog keys.
var (
	reasonSitemapNotConfigured = i18n.Define("uihost.sitemap_not_configured", "sitemap not configured")
	reasonRobotsNotConfigured  = i18n.Define("uihost.robots_not_configured", "robots not configured")
	reasonEmbedSSE             = i18n.Define("embed.sse_grant", "embed: SSE cannot be authenticated by a grant: EventSource sends no request headers, so the frame's grant never reaches this endpoint. Use data-cui-poll (or widget Builder.Poll) for live updates inside a frame.")
	reasonPWANotConfigured     = i18n.Define("uihost.pwa_not_configured", "pwa not configured")
	reasonSessionMissing       = i18n.Define("uihost.session_missing", "missing session parameter")
	reasonUnknownSession       = i18n.Define("uihost.unknown_session", "unknown session")
	reasonWidgetNotFound       = i18n.Define("uihost.widget_not_found", "widget not found: {{id}}")
	reasonActionProcessed      = i18n.Define("uihost.action_processed", "Server action processed")
	reasonActionAcknowledged   = i18n.Define("uihost.action_acknowledged", "Server action acknowledged (no handler)")
)
