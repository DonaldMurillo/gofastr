package uihost

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

type manifestEntry struct {
	body, hash string
}

func (ds *UIHost) manifestJS(ctx context.Context) (body, hash string) {
	m := ds.manifestFor(ds.renderLocale(ctx))
	return m.body, m.hash
}

func (ds *UIHost) manifestFor(lr localeRender) manifestEntry {
	return ds.manifests.get(lr.key, func() manifestEntry {
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
		if err == nil {
			b = append(b, acts...)
		} else {
			b = append(b, "{}"...)
		}
		b = append(b, ";\n"...)
		if rs := strings.TrimSpace(ds.routeScriptFor(lr)); rs != "" {
			b = append(b, rs...)
			b = append(b, '\n')
		}
		if loc := ds.localesJSON(""); loc != nil {
			b = append(b, "window.__gofastr_locales="...)
			b = append(b, loc...)
			b = append(b, ";\n"...)
		}
		body := string(b)
		return manifestEntry{body: body, hash: hashStrings(body)}
	})
}

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
