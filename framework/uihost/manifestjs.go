package uihost

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

// /__gofastr/manifest.js is the externalized form of the per-page inline
// data blocks: the component catalog (every registered component × its
// stylesheet path/version/load mode) and the runtime-module hash
// manifest, plus the compiled-action hash map the action loader reads.
// Inlining them cost ~2.5–3 KB gz of head JSON on EVERY page render and
// every full-shell navigation; as one external content-addressed script
// they are fetched once per deploy and revalidate with a 304 after.
//
// It is a classic script injected immediately BEFORE runtime.js, so its
// globals are assigned by the time the kernel boots — the kernel and the
// module loader already prefer the window globals over the inline blocks.
// Export mode (static sites, the PWA offline shell) and theme-variant
// pages keep the inline blocks: exports must be self-contained files,
// and variant catalogs vary per request.
//
// The manifest carries only public data: component names, style paths,
// and content hashes. It is deliberately ungated.
func (ds *UIHost) manifestJS() (body, hash string) {
	ds.manifestOnce.Do(func() {
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
		ds.manifestBody = string(b)
		ds.manifestHash = hashStrings(ds.manifestBody)
	})
	return ds.manifestBody, ds.manifestHash
}

func (ds *UIHost) handleManifestJS(w http.ResponseWriter, r *http.Request) {
	body, hash := ds.manifestJS()
	serveVersionedText(w, r, "application/javascript; charset=utf-8", body, hash, false)
}
