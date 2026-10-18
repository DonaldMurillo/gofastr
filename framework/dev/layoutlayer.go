package dev

import (
	_ "embed"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// LayoutLayerScriptURL serves the dev layout layer: a script that draws
// the page's layout layers, slots, outlets, areas, owned-style scopes,
// islands and widgets as labeled outlines, and flags scroll boxes and
// elements that run past the viewport. Alt+Shift+L toggles it; the choice
// survives livereload's reloads.
const LayoutLayerScriptURL = "/__devlayout.js"

//go:embed layoutlayer.js
var layoutLayerJS []byte

// LayoutLayerEnabled reports whether the layout layer should auto-wire:
// the same gate as livereload, with GOFASTR_DEV_LAYOUT=0 as its opt-out.
func LayoutLayerEnabled() bool {
	return devFeatureEnabled("GOFASTR_DEV_LAYOUT")
}

var (
	layoutRegisteredMu sync.Mutex
	layoutRegistered   = map[*router.Router]bool{}
)

// RegisterLayoutLayer installs the layout layer's script route on r and
// adds the script to the dev injector, so pages uihost does not render
// get it too. Idempotent, and a no-op when the host already serves the
// URL itself.
func RegisterLayoutLayer(r *router.Router) {
	if r == nil {
		return
	}
	layoutRegisteredMu.Lock()
	if layoutRegistered[r] {
		layoutRegisteredMu.Unlock()
		return
	}
	layoutRegistered[r] = true
	layoutRegisteredMu.Unlock()

	for _, rt := range r.Routes() {
		if rt.Pattern == LayoutLayerScriptURL {
			return
		}
	}
	r.Get(LayoutLayerScriptURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The labels carry non-ASCII; a page with no charset would
		// otherwise decode the script as windows-1252.
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(layoutLayerJS)
	}))
	addDevScript(r, LayoutLayerScriptURL)
}

// MaybeRegisterLayoutLayer calls RegisterLayoutLayer only when
// LayoutLayerEnabled reports true. framework.NewApp calls it beside
// MaybeRegisterLiveReload.
func MaybeRegisterLayoutLayer(r *router.Router) {
	if LayoutLayerEnabled() {
		RegisterLayoutLayer(r)
	}
}
