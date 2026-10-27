package uihost

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

// /__gofastr/manifest.js is the externalized form of the per-page inline
// data blocks: the component catalog (every registered component × its
// stylesheet path/version/load mode) and the runtime-module hash
// manifest, plus the compiled-action hash map the action loader reads
// and the route graph the client router reads.
// Inlining them cost ~2.5–3 KB gz of head JSON on EVERY page render and
// every full-shell navigation; as one external content-addressed script
// they are fetched once per deploy and revalidate with a 304 after.
//
// It is a classic script injected immediately BEFORE runtime.js, so its
// globals are assigned by the time the kernel boots. The behaviours
// global carries registered behaviours' markers (registry.RegisterBehavior)
// so the kernel's scanner learns them the way the loader learns hashes. The kernel and the
// module loader already prefer the window globals over the inline blocks.
// Export mode (static sites, the PWA offline shell) and theme-variant
// pages keep the inline blocks: exports must be self-contained files,
// and variant catalogs vary per request.
//
// The route graph carries each route's loading content in the reader's
// language, so an app with a translator has one body per catalog
// locale, each with its own hash. A page links the body of its own
// locale (?v=<hash>), and the handler serves the body that hash names:
// the request for the script carries no locale prefix, so the hash is
// what says which page asked. An app without a translator has one body.
//
// The manifest carries only public data: component names, style paths,
// and content hashes. It is deliberately ungated.
type manifestEntry struct {
	body, hash string
}

// manifestJS returns the manifest body and hash for the reader's locale.
func (ds *UIHost) manifestJS(ctx context.Context) (body, hash string) {
	m := ds.manifestFor(ds.renderLocale(ctx))
	return m.body, m.hash
}

func (ds *UIHost) manifestFor(lr localeRender) manifestEntry {
	return ds.manifests.get(lr.key, func() manifestEntry {
		// The body freezes the route graph, document scripts included,
		// so registration closes here as it does on the first render: a
		// script added later would be missing from the graph.
		ds.markServingBegun()
		var b []byte
		b = append(b, "window.__gofastr_catalog="...)
		if cat := catalogJSON(ds, ds.activeTheme(), ""); cat != nil {
			b = append(b, cat...)
		} else {
			b = append(b, "{}"...)
		}
		b = append(b, ";\nwindow.__gofastr_runtime_modules="...)
		if mods := widget.RuntimeModuleManifestJSON(); mods != nil {
			b = append(b, mods...)
		} else {
			b = append(b, "{}"...)
		}
		b = append(b, ";\nwindow.__gofastr_behaviors="...)
		if beh := runtime.BehaviorsJSON(); beh != nil {
			b = append(b, beh...)
		} else {
			b = append(b, "{}"...)
		}
		b = append(b, ";\nwindow.__gofastr_actions="...)
		ds.mu.RLock()
		acts, err := json.Marshal(ds.actionHash)
		ds.mu.RUnlock()
		if err == nil && len(ds.actionHash) > 0 {
			b = append(b, acts...)
		} else {
			b = append(b, "{}"...)
		}
		b = append(b, ";\n"...)
		// The route graph rides here too on live pages (the page head
		// drops its inline block), so a 185-route app stops shipping
		// ~15 KB of route JSON in every document.
		if rs := strings.TrimSpace(ds.routeScriptFor(lr)); rs != "" {
			b = append(b, rs...)
			b = append(b, '\n')
		}
		// The locale set of an app that routes by locale prefix: the
		// runtime reads it to look routes up without the prefix and to
		// load a document when a link changes the reader's locale.
		if loc := ds.localesJSON(""); loc != nil {
			b = append(b, "window.__gofastr_locales="...)
			b = append(b, loc...)
			b = append(b, ";\n"...)
		}
		body := string(b)
		return manifestEntry{body: body, hash: hashStrings(body)}
	})
}

// manifestByHash finds the body whose hash is v among the app's locales,
// building any not built yet: a page another replica rendered can name
// a locale this one has not served. The search is bounded by the
// catalog's locale set, never by v.
func (ds *UIHost) manifestByHash(ctx context.Context, v string) (manifestEntry, bool) {
	for _, lr := range ds.renderLocales(ctx) {
		if m := ds.manifestFor(lr); m.hash == v {
			return m, true
		}
	}
	return manifestEntry{}, false
}

func (ds *UIHost) handleManifestJS(w http.ResponseWriter, r *http.Request) {
	m := ds.manifestFor(ds.renderLocale(r.Context()))
	if v := r.URL.Query().Get("v"); v != "" && v != m.hash {
		if hit, ok := ds.manifestByHash(r.Context(), v); ok {
			m = hit
		}
	}
	serveVersionedText(w, r, "application/javascript; charset=utf-8", m.body, m.hash, false)
}
