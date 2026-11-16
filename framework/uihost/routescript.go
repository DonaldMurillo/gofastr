package uihost

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// routeInfoJSON is the JSON shape sent to the browser as __gofastr_routes.
// Every visitor downloads it (in manifest.js on live pages, inline in
// exports), so it carries only what the runtime reads: no titles or
// descriptions (a navigation takes its title from the response).
type routeInfoJSON struct {
	Path string `json:"path"`
	// Preload is the route's prefetch mode ("hover"/"visible"/"eager");
	// absent means never. Any non-empty value in the manifest loads the
	// prefetch runtime module.
	Preload string `json:"preload,omitempty"`
	// Layouts is the route's layout chain as layer keys, outermost →
	// innermost (app.LayoutLayer.Key). The runtime compares it against the
	// DOM's data-cui-layout-key spine to swap at the deepest shared layer.
	Layouts []string `json:"layouts,omitempty"`
	// Deferred the route's deferred outlet
	// addresses — every fill the client must fetch as its own part
	// request (X-Gofastr-Part) beside the page request. Absent when no
	// outlet in the chain defers.
	Deferred []string `json:"deferred,omitempty"`
	// DocScripts lists the document-lifetime external scripts (src
	// values, RegisterDocumentScript) in scope for this route. The
	// client runtime compares the destination's set against the live
	// document's data-cui-doc scripts and performs a real document load
	// when they differ: removing a script tag does not revoke
	// document-installed capabilities (WebMCP's navigator.modelContext
	// tools), and a partial swap never runs a body script.
	DocScripts []string `json:"docScripts,omitempty"`
	// Loading is the route's swap-slot
	// loading content as rendered HTML plus its After/Min in whole
	// milliseconds, from the screen's (or its group's) WithLoading
	// declaration. Absent when no route declares one. EVERY page pays
	// these bytes (the manifest travels with every document), which is
	// why per-outlet loading templates ride beside their cells instead.
	Loading      string `json:"loading,omitempty"`
	LoadingAfter int    `json:"loadingAfter,omitempty"`
	LoadingMin   int    `json:"loadingMin,omitempty"`
	// Redirect is the target path (or pattern) for a redirect entry.
	// Empty for screens. The client-side router rewrites a navigation to
	// this entry's path to Redirect without a server round-trip.
	Redirect string `json:"redirect,omitempty"`
	// Intercept marks a screen that presents as an overlay when reached
	// by a soft navigation from the named origin route. Absent for
	// ordinary screens, so its presence anywhere in the manifest is also
	// the trigger that loads the intercept runtime module.
	Intercept *interceptJSON `json:"intercept,omitempty"`
}

// interceptJSON mirrors app.Intercept for the browser. `as` is the
// string form ("drawer"/"sheet"), not the Go enum's number: the
// manifest is a wire format and must not encode iota positions.
type interceptJSON struct {
	From string   `json:"from"`
	Also []string `json:"also,omitempty"`
	As   string   `json:"as"`
}

// buildRouteScript returns the __gofastr_routes script body for the
// reader's locale, computed ONCE per catalog locale and cached. The
// route table is static after wire time (screens register at App.Mount,
// before any page render), so rebuilding + re-marshaling it on every
// request was pure waste; only the loading content in it differs by
// locale. CSS chunk names are auto-derived from the screen path.
func (ds *UIHost) buildRouteScript(ctx context.Context) string {
	return ds.routeScriptFor(ds.renderLocale(ctx))
}

func (ds *UIHost) routeScriptFor(lr localeRender) string {
	return ds.routeScripts.get(lr.key, func() string {
		return ds.buildRouteScriptUncached(lr.ctx)
	})
}

// buildRouteScriptUncached is the per-call marshal; buildRouteScript
// caches it. ctx is the locale the loading content renders in.
func (ds *UIHost) buildRouteScriptUncached(ctx context.Context) string {
	routes := ds.App.Routes()
	if len(routes) == 0 {
		return ""
	}
	// Filter before sizing infos, not after: sized from the pre-filter
	// count, every NoSPA route left a trailing zero-value entry that
	// marshalled as {"path":""} and reached the client as a real route.
	kept := routes[:0]
	for _, r := range routes {
		if !r.NoSPA {
			kept = append(kept, r)
		}
	}
	routes = kept
	if len(routes) == 0 {
		return ""
	}
	infos := make([]routeInfoJSON, len(routes))
	for i, r := range routes {
		infos[i] = routeInfoJSON{
			Path:     r.Path,
			Preload:  r.Preload,
			Layouts:  r.Layouts,
			Deferred: r.Deferred,
			Redirect: r.RedirectTo,
		}
		// Document-lifetime scripts in scope for this route, sorted so
		// the manifest bytes are deterministic. The scope sees the
		// route PATTERN here and the concrete path at render time;
		// prefix-style predicates answer both the same way.
		for _, s := range ds.extraScripts {
			if s.scope != nil && s.scope(r.Path) {
				infos[i].DocScripts = append(infos[i].DocScripts, s.src)
			}
		}
		sort.Strings(infos[i].DocScripts)
		// the route's swap-slot loading
		// content rides the manifest.
		if r.Loading != nil {
			infos[i].Loading = r.Loading.HTML(ctx)
			infos[i].LoadingAfter = r.Loading.After
			infos[i].LoadingMin = r.Loading.Min
		}
		if r.Intercept != nil {
			infos[i].Intercept = &interceptJSON{
				From: r.Intercept.From,
				Also: r.Intercept.AlsoFrom,
				As:   r.Intercept.As.String(),
			}
		}
	}
	ds.routeMarshalCount.Add(1)
	rgJSON, _ := json.Marshal(infos)
	// JS body only, no <script> wrapper. Live pages get it inside
	// /__gofastr/manifest.js (CSP-safe under default-src 'self');
	// exports and the PWA offline shell inline it as #gofastr-routes.
	return fmt.Sprintf("window.__gofastr_routes = %s;", string(rgJSON))
}

// hasInterceptingRoute reports whether any registered screen presents as
// an overlay for soft navigations from a declared origin. Gates both the
// overlay CSS and, via the route manifest, the intercept runtime module.
func (ds *UIHost) hasInterceptingRoute() bool {
	for _, r := range ds.App.Routes() {
		if r.Intercept != nil {
			return true
		}
	}
	return false
}

// routesJSONScript embeds the route graph as inert JSON. Same model
// as catalogJSONScript. Returns "" when no routes are registered
// (e.g. a host used standalone in tests without a real app). The
// loading content in it speaks the locale on ctx.
func routesJSONScript(ctx context.Context, ds *UIHost) string {
	body := strings.TrimSpace(ds.buildRouteScript(ctx))
	if body == "" {
		return ""
	}
	// buildRouteScript returns `window.__gofastr_routes = <JSON>;`.
	// Strip the wrapper to get just the JSON payload.
	body = strings.TrimPrefix(body, "window.__gofastr_routes =")
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, ";")
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	return `<script type="application/json" id="gofastr-routes">` +
		escapeJSONForScript([]byte(body)) +
		`</script>`
}
