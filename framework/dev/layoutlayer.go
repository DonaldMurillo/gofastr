package dev

import (
	_ "embed"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// LayoutLayerScriptURL serves the dev layout layer, a script that outlines
// the page's layouts, slots, islands and widgets (Alt+Shift+L toggles it).
const LayoutLayerScriptURL = "/__devlayout.js"

//go:embed layoutlayer.js
var layoutLayerJS []byte

// LayoutLayerEnabled reports whether the layout layer should auto-wire: the
// livereload gate with GOFASTR_DEV_LAYOUT as its opt-out.
func LayoutLayerEnabled() bool {
	return devFeatureEnabled("GOFASTR_DEV_LAYOUT")
}

var (
	layoutRegisteredMu sync.Mutex
	layoutRegistered   = map[*router.Router]bool{}
)

// RegisterLayoutLayer serves the layout layer on r and adds it to the HTML
// injector. It is idempotent and yields to a host route at the URL.
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
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(layoutLayerJS)
	}))
	addDevScript(r, LayoutLayerScriptURL)
}

// MaybeRegisterLayoutLayer calls RegisterLayoutLayer when LayoutLayerEnabled.
func MaybeRegisterLayoutLayer(r *router.Router) {
	if LayoutLayerEnabled() {
		RegisterLayoutLayer(r)
	}
}
