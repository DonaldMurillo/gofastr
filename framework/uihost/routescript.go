package uihost

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type routeInfoJSON struct {
	Path         string         `json:"path"`
	Preload      string         `json:"preload,omitempty"`
	Layouts      []string       `json:"layouts,omitempty"`
	Deferred     []string       `json:"deferred,omitempty"`
	DocScripts   []string       `json:"docScripts,omitempty"`
	Loading      string         `json:"loading,omitempty"`
	LoadingAfter int            `json:"loadingAfter,omitempty"`
	LoadingMin   int            `json:"loadingMin,omitempty"`
	Redirect     string         `json:"redirect,omitempty"`
	Intercept    *interceptJSON `json:"intercept,omitempty"`
}

type interceptJSON struct {
	From string   `json:"from"`
	Also []string `json:"also,omitempty"`
	As   string   `json:"as"`
}

func (ds *UIHost) buildRouteScript(ctx context.Context) string {
	return ds.routeScriptFor(ds.renderLocale(ctx))
}

func (ds *UIHost) routeScriptFor(lr localeRender) string {
	return ds.routeScripts.get(lr.key, func() string {
		return ds.buildRouteScriptUncached(lr.ctx)
	})
}

func (ds *UIHost) buildRouteScriptUncached(ctx context.Context) string {
	routes := ds.App.Routes()
	if len(routes) == 0 {
		return ""
	}
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
		for _, s := range ds.extraScripts {
			if s.scope != nil && s.scope(r.Path) {
				infos[i].DocScripts = append(infos[i].DocScripts, s.src)
			}
		}
		sort.Strings(infos[i].DocScripts)
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
	return fmt.Sprintf("window.__gofastr_routes = %s;", string(rgJSON))
}

func (ds *UIHost) hasInterceptingRoute() bool {
	for _, r := range ds.App.Routes() {
		if r.Intercept != nil {
			return true
		}
	}
	return false
}

func routesJSONScript(ctx context.Context, ds *UIHost) string {
	body := strings.TrimSpace(ds.buildRouteScript(ctx))
	if body == "" {
		return ""
	}
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
