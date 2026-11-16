package uihost

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ScreenResponse is the status and cache policy of [UIHost.RenderScreen].
// The zero value renders a 200.
type ScreenResponse struct {
	// Status is the HTTP status. Zero takes the component's
	// [ScreenStatusCode], else what its render set through app.SetStatus,
	// else 200; a code outside 100-999 is sent as 500.
	Status int

	// CacheControl replaces "private, no-store" on the partial (client
	// navigation) answer. The full document always answers no-store with
	// Vary: Cookie.
	CacheControl string
}

const screenCacheControlDefault = "private, no-store"

// RenderScreen writes comp as a page in the default layout with the
// status and cache policy resp names, so middleware can answer a guarded
// route with a branded recovery screen. It authorizes nothing. A client
// navigation gets the bare body, title and signal seed under the same
// status and mints no session; a document load gets the full page.
//
//	if m, ok := app.MatchFromContext(r.Context()); ok && expired(m.Param("id")) {
//		host.RenderScreen(w, r, goneScreen, uihost.ScreenResponse{Status: http.StatusGone})
//		return
//	}
func (ds *UIHost) RenderScreen(w http.ResponseWriter, r *http.Request, comp component.Component, resp ScreenResponse) {
	app.RefuseRetiredScreenMethods("RenderScreen", comp)
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
		i18n.HTTPError(w, r, status, i18n.StatusPhrase(status))
		return
	}

	ctx, recorded := app.TrackStatus(store.WithValues(app.WithRequest(r.Context(), r)))
	body, err := component.SafeRenderCtx(ctx, comp)
	if err != nil {
		w.Header().Set("Cache-Control", cacheControl)
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	if resp.Status == 0 && status == http.StatusOK {
		if code := recorded(); code != 0 {
			status = code
		}
	}

	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	appName := "GoFastr"
	if ds.App != nil && ds.App.Name != "" {
		appName = ds.App.Name
	}
	title := fmt.Sprintf("%d: %s", status, appName)
	if status == http.StatusOK {
		title = appName
	}
	if t, ok := comp.(app.ScreenTitler); ok {
		if name := t.ScreenTitle(r.Context()); name != "" {
			title = name
			if suffix := " — " + appName; !strings.HasSuffix(title, suffix) {
				title += suffix
			}
		}
	}

	if r.Header.Get("X-Gofastr-Navigate") == "1" {
		if staleMarkupClient(r) {
			w.Header().Del("Cache-Control")
			writeStaleMarkupReload(w, r, routePath(r), "")
			return
		}
		w.Header().Set("X-Gofastr-Partial", "true")
		w.Header().Set("X-Gofastr-Title", url.PathEscape(textsafe.StripInvisible(title)))
		if ds.App != nil {
			body = render.Join(body, ds.partialHead(ctx, routePath(r), comp))
		}
		w.WriteHeader(status)
		fmt.Fprint(w, partialSeedIsland(ctx, string(body))+string(body))
		return
	}

	if ds.App != nil && ds.App.Router != nil {
		if layout := ds.App.Router.GetDefaultLayout(); layout != nil {
			body = layout.WrapCtx(r.Context(), body)
		}
	}
	ds.finishPageDocument(w, r, ctx,
		render.HTML(ds.documentShell(ctx, r.URL.Path, title, string(body))), r.URL.Path, comp, status)
}
