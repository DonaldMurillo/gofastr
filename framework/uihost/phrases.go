package uihost

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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

// The words of the built-in 404, 405, 500 and offline pages.
var (
	pageBackHome        = i18n.Define("uihost.back_home", "Back to home")
	pageNotFoundTitle   = i18n.Define("uihost.not_found_title", "404: Page not found")
	pageNotFoundBody    = i18n.Define("uihost.not_found_body", "This page does not exist.")
	pageNoRoute         = i18n.Define("uihost.no_route", "No route matched {{path}}.")
	pageErrorTitle      = i18n.Define("uihost.error_title", "Something went wrong")
	pageErrorBody       = i18n.Define("uihost.error_body", "This page could not be loaded. Try again in a moment.")
	pageMethodTitle     = i18n.Define("uihost.method_title", "405: Method Not Allowed")
	pageMethodBody      = i18n.Define("uihost.method_body", "Method {{method}} is not allowed for {{path}}.")
	pageMethodAllowed   = i18n.Define("uihost.method_allowed", "Allowed methods: {{methods}}.")
	pageOfflineTitle    = i18n.Define("uihost.offline_title", "You're offline")
	pageOfflineBody     = i18n.Define("uihost.offline_body", "This page isn't available without a connection. Reconnect and try again.")
	pageOfflineDocTitle = i18n.Define("uihost.offline_doc_title", "Offline — {{app}}")

	pageNotFoundDocTitle    = i18n.Define("uihost.not_found_doc_title", "Not found: {{app}}")
	pageServerErrorDocTitle = i18n.Define("uihost.server_error_doc_title", "Server error: {{app}}")
	pageServerErrorTitle    = i18n.Define("uihost.server_error_title", "500: Server error")

	// The notice a tab opened before a deploy gets in place of the page
	// it asked for (writeStaleMarkupReload).
	pageAppUpdated = i18n.Define("uihost.app_updated", "This app was updated.")
	pageReloadPage = i18n.Define("uihost.reload_page", "Reload the page.")
)

// The RFC 9457 problem documents a machine client gets for a 404 or a
// failed render: the title names the status, the detail says what
// happened, and the reason member carries the detail's stable code.
var (
	problemNotFound     = i18n.Define("uihost.problem_not_found", "Not Found")
	problemNoRoute      = i18n.Define("uihost.problem_no_route", "No route matches this request.")
	problemServerError  = i18n.Define("uihost.problem_server_error", "Internal Server Error")
	problemRenderFailed = i18n.Define("uihost.problem_render_failed", "The page failed to render.")

	problemMethodNotAllowed = i18n.Define("uihost.problem_method_not_allowed", "Method Not Allowed")
	problemMethodRefused    = i18n.Define("uihost.problem_method_refused", "This address does not accept the request's method; the Allow header lists the ones it does.")
)

// problem is an RFC 9457 problem document with the framework's reason
// extension member.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Reason string `json:"reason"`
}

// newProblem says title and detail in ctx's language.
func newProblem(ctx context.Context, status int, title, detail i18n.Phrase) problem {
	return problem{
		Type:   "about:blank",
		Title:  title.Text(ctx),
		Status: status,
		Detail: detail.Text(ctx),
		Reason: detail.Code(),
	}
}

// phraseAround renders p in ctx's language with the {{name}} placeholder
// replaced by node (a <code> path, say), the rest filled from params as
// text. The template is cut before it is filled, so a value can never
// pose as the placeholder.
func phraseAround(ctx context.Context, p i18n.Phrase, name string, node render.HTML, params map[string]any) render.HTML {
	before, after, ok := strings.Cut(p.Text(ctx), "{{"+name+"}}")
	if !ok {
		return render.Text(i18n.FillContext(ctx, before, params))
	}
	return render.Join(render.Text(i18n.FillContext(ctx, before, params)), node, render.Text(i18n.FillContext(ctx, after, params)))
}
