package uihost

import (
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
// The manifest carries only public data: component names, style paths,
// and content hashes. It is deliberately ungated.
func (ds *UIHost) manifestJS() (body, hash string) {
	ds.manifestOnce.Do(func() {
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
		if rs := strings.TrimSpace(ds.buildRouteScript()); rs != "" {
			b = append(b, rs...)
			b = append(b, '\n')
		}
		ds.manifestBody = string(b)
		ds.manifestHash = hashStrings(ds.manifestBody)
	})
	return ds.manifestBody, ds.manifestHash
}

func (ds *UIHost) handleManifestJS(w http.ResponseWriter, r *http.Request) {
	body, hash := ds.manifestJS()
	serveVersionedText(w, r, "application/javascript; charset=utf-8", body, hash, false)
}
