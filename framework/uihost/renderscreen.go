package uihost

import (
	"fmt"
	stdhtml "html"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// ScreenResponse names the response-wide HTTP semantics for
// [UIHost.RenderScreen]. The zero value renders a 200 with the default
// cache policy.
type ScreenResponse struct {
	// Status is the HTTP status code to send. Zero means 200, unless
	// the component implements [ScreenStatusCode], whose value then
	// applies with the same contract a registered screen has. An
	// out-of-range code (<100 or >999) is replaced with 500 rather
	// than panicking inside net/http.
	Status int

	// CacheControl overrides the default "private, no-store".
	// Recovery pages are per-user; only set something else with a
	// reason.
	CacheControl string
}

// screenCacheControlDefault is what RenderScreen sends when
// ScreenResponse.CacheControl is empty. A recovery screen is rendered
// for one caller's auth state, so a shared cache must never replay it.
const screenCacheControlDefault = "private, no-store"

// RenderScreen writes comp as a full-chrome page (default layout,
// runtime.js, theme) with the status and cache policy named by resp.
// It is the supported way for application middleware to answer a
// guarded screen route with a branded recovery screen instead of
// middleware plain text, without registering a sentinel path:
//
//	match, ok := app.MatchFromContext(r.Context())
//	if ok && sessions.Expired(match.Param("sessionId")) {
//		host.RenderScreen(w, r, sessionGoneScreen, uihost.ScreenResponse{
//			Status: http.StatusGone,
//		})
//		return
//	}
//
// RenderScreen renders; it authorizes nothing. The caller (middleware)
// owns the decision, so it also owns status choice: the guide is
// 401/403 for an authentication failure on a route that exists, 410
// (or a 404 through ScreenStatusCode) for a route that resolved but
// whose resource is gone, and nothing here for an unknown route — the
// host's WithNotFoundScreen 404 stays truthful because RenderScreen is
// only reached when a guard chose to answer.
//
// Full and partial (X-Gofastr-Navigate) requests get the same status
// and cache policy. The partial arm carries the bare component body:
// the runtime surfaces a non-2xx partial as a navigation error and
// stays on the current page, while a full load renders the branded
// page. Neither arm mints a session or sets a cookie.
func (ds *UIHost) RenderScreen(w http.ResponseWriter, r *http.Request, comp component.Component, resp ScreenResponse) {
	status := resp.Status
	if status == 0 {
		if sc, ok := comp.(ScreenStatusCode); ok {
			status = sc.ScreenStatusCode()
		}
	}
	if status == 0 {
		status = http.StatusOK
	}
	if status < 100 || status > 999 {
		status = http.StatusInternalServerError
	}
	cacheControl := resp.CacheControl
	if cacheControl == "" {
		cacheControl = screenCacheControlDefault
	}

	if comp == nil {
		w.Header().Set("Cache-Control", cacheControl)
		http.Error(w, http.StatusText(status), status)
		return
	}

	body, err := component.SafeRenderCtx(r.Context(), comp)
	if err != nil {
		w.Header().Set("Cache-Control", cacheControl)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Client-side navigation: same status, same cache policy, bare
	// content. No layout, no chrome, and deliberately no session
	// minting — the partial page path's re-mint would attach a fresh
	// Set-Cookie to an auth-failure response.
	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		w.WriteHeader(status)
		fmt.Fprint(w, string(body))
		return
	}

	if ds.App != nil && ds.App.Router != nil {
		if layout := ds.App.Router.GetDefaultLayout(); layout != nil {
			body = layout.Wrap(body)
		}
	}
	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	title := fmt.Sprintf("%d: %s", status, appName)
	if status == http.StatusOK {
		title = appName
	}
	// Same convention as the page pipeline: "<page> — <app>".
	if t, ok := comp.(app.ScreenTitler); ok {
		if name := t.ScreenTitle(); name != "" {
			title = name + " — " + appName
		}
	}
	shell := fmt.Sprintf(
		`<!DOCTYPE html><html lang="%s"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>%s</title></head><body>%s</body></html>`,
		stdhtml.EscapeString(ds.EffectiveLang()), stdhtml.EscapeString(title), string(body))
	page := ds.injectChrome(shell, r.URL.Path, "", "")
	w.WriteHeader(status)
	fmt.Fprint(w, page)
}
