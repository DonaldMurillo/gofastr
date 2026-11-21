package uihost

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// CompileActions compiles a component's action methods to JS, caches the
// result under componentID (first call wins) and stores the registry the
// server-action endpoint invokes. An action compiled by a direct call has
// no owning screen, so no screen Policy runs before its Go handler.
func (ds *UIHost) CompileActions(componentID string, comp component.Component) string {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if js, ok := ds.actionJS[componentID]; ok {
		return js
	}

	if ic, ok := comp.(component.InteractiveComponent); ok {
		actions := component.ExtractActions(ic)
		if actions != nil {
			js := actionsToJS(componentID, actions)
			ds.actionJS[componentID] = js
			ds.actionHash[componentID] = hashStrings(js)
			ds.actionHandlers[componentID] = actions
			ds.actionComps[componentID] = comp
			return js
		}
	}
	return ""
}

// AutoCompileActions compiles the actions of every registered screen that
// implements InteractiveComponent, under the id its ComponentID method
// declares or one derived from the route path. It panics when two routes
// derive one id.
func (ds *UIHost) AutoCompileActions() {
	claimed := make(map[string]string, len(ds.App.Routes()))
	for _, route := range ds.App.Routes() {
		screen, ok := ds.App.Router.ScreenByPattern(route.Path)
		if !ok {
			continue
		}
		if _, ok := screen.Component.(component.InteractiveComponent); ok {
			var id string
			if cid, ok := screen.Component.(interface{ ComponentID() string }); ok {
				id = cid.ComponentID()
			} else {
				id = pathToActionID(route.Path)
			}
			if prev, taken := claimed[id]; taken && prev != route.Path {
				panic(fmt.Sprintf(
					"uihost: screens %q and %q both derive server-action id %q; "+
						"one screen's Go handlers would be unreachable and its data-action clicks "+
						"would run the other's. Give one of them an explicit ComponentID() string "+
						"method or rename the route.",
					prev, route.Path, id))
			}
			claimed[id] = route.Path
			ds.CompileActions(id, screen.Component)
			ds.mu.Lock()
			ds.actionScreens[id] = screen
			ds.mu.Unlock()
		}
	}
}

func pathToActionID(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home"
	}
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name
}

// GetActionJS returns all compiled action JS concatenated, sorted by
// component id.
func (ds *UIHost) GetActionJS() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	sb := borrowBuilder()
	defer returnBuilder(sb)
	for _, name := range slices.Sorted(maps.Keys(ds.actionJS)) {
		sb.WriteString(ds.actionJS[name])
		sb.WriteString("\n")
	}
	return sb.String()
}

// HasActions reports whether any component has compiled action JS,
// without building the bundle.
func (ds *UIHost) HasActions() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.actionJS) > 0
}

func (ds *UIHost) actionScriptTags(page string) []string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if len(ds.actionJS) == 0 {
		return nil
	}
	ids := make([]string, 0, len(ds.actionJS))
	for id := range ds.actionJS {
		if strings.Contains(page, `data-component="`+id+`"`) ||
			strings.Contains(page, `data-widget="`+id+`"`) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	tags := make([]string, len(ids))
	for i, id := range ids {
		tags[i] = `<script defer src="/__gofastr/widget/` + id + `.js?v=` + ds.actionHash[id] + `"></script>`
	}
	return tags
}

func (ds *UIHost) handleActionsJS(w http.ResponseWriter, r *http.Request) {
	if !ds.requireSessionOrEmbedGrant(w, r) {
		return
	}
	js := ds.GetActionJS()
	serveVersionedText(w, r, "application/javascript; charset=utf-8", js, hashStrings(js), true)
}

func (ds *UIHost) handleServerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, r)
		return
	}
	if rejectCrossOrigin(w, r) {
		return
	}
	if r.ContentLength > maxMutatingBodyBytes {
		i18n.HTTPError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
		return
	}

	var body struct {
		Action      string            `json:"action"`
		Params      map[string]string `json:"params"`
		Session     string            `json:"session"`
		ComponentID string            `json:"componentId"`
		Page        string            `json:"page"`
	}
	if !decodeBounded(w, r, &body) {
		return
	}

	componentID := body.ComponentID
	actionName := body.Action

	ds.mu.RLock()
	reg, ok := ds.actionHandlers[componentID]
	ds.mu.RUnlock()

	if !ok || reg == nil {
		http.NotFound(w, r)
		return
	}

	actionDef, found := reg.Get(actionName)
	if !found {
		http.NotFound(w, r)
		return
	}

	if _, ok := ds.requireValidSession(w, r); !ok {
		return
	}

	ds.mu.RLock()
	screen := ds.actionScreens[componentID]
	ds.mu.RUnlock()
	if screen != nil {
		ctx := app.WithRequest(r.Context(), r)
		var d app.Decision
		judged := false
		if body.Page != "" {
			page := app.RoutePath(body.Page)
			if m, ok := ds.App.Router.MatchFor(page); ok && m.ScreenID() == screen.Path {
				var err error
				d, _, err = ds.App.PagePolicy(app.WithMatch(ctx, m), page)
				if err != nil {
					d = app.Decision{Kind: app.DecisionBlock, Status: http.StatusForbidden}
					if errors.Is(err, app.ErrNotFound) {
						d.Status = http.StatusNotFound
					}
				}
				judged = true
			}
		}
		if !judged {
			d = ds.App.PatternPolicy(ctx, screen)
		}
		if d.Kind != app.DecisionAllow {
			status := http.StatusForbidden
			if d.Kind == app.DecisionBlock && d.Status >= 400 && d.Status < 600 {
				status = d.Status
			}
			i18n.HTTPError(w, r, status, i18n.StatusPhrase(status))
			return
		}
	}

	if actionDef.Handler != nil {
		ctx := component.NewComponentContextFor(r.Context(), actionName, "", body.Params)
		actionDef.Handler(ctx)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"action":  actionName,
			"message": reasonActionProcessed.Text(r.Context()),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"action":  actionName,
		"message": reasonActionAcknowledged.Text(r.Context()),
	})
}

func (ds *UIHost) handleWidgetJS(w http.ResponseWriter, r *http.Request) {
	if !ds.requireSessionOrEmbedGrant(w, r) {
		return
	}
	widgetID := strings.TrimPrefix(r.URL.Path, "/__gofastr/widget/")
	widgetID = strings.TrimSuffix(widgetID, ".js")

	ds.mu.RLock()
	js, ok := ds.actionJS[widgetID]
	hash := ds.actionHash[widgetID]
	ds.mu.RUnlock()

	if !ok {
		i18n.HTTPError(w, r, http.StatusNotFound, reasonWidgetNotFound, map[string]any{"id": widgetID})
		return
	}
	serveVersionedText(w, r, "application/javascript; charset=utf-8", js, hash, true)
}

func actionsToJS(componentID string, reg *component.ActionRegistry) string {
	if !reg.HasActions() {
		return ""
	}

	sb := borrowBuilder()
	defer returnBuilder(sb)
	sb.WriteString(fmt.Sprintf("// Component: %s\n", componentID))
	sb.WriteString("(() => {\n")
	sb.WriteString(fmt.Sprintf("  const id = %q;\n", componentID))
	sb.WriteString("  const G = window.__gofastr;\n")
	sb.WriteString("  const handlers = {\n")

	first := true
	for _, action := range reg.All() {
		if !first {
			sb.WriteString(",\n")
		}
		first = false

		body := strings.TrimSpace(action.ClientJS)
		if body == "" {
			body = fmt.Sprintf("// no client handler for %q", action.Event)
		}

		body = strings.ReplaceAll(body, "G.serverAction(", fmt.Sprintf("G._serverActionFor(%q, ", componentID))

		sb.WriteString(fmt.Sprintf("    %q: (params) => {\n      %s\n    }", action.Event, body))
	}

	sb.WriteString("\n  };\n")
	sb.WriteString("  G.register(id, handlers);\n")
	sb.WriteString("})();\n")

	return sb.String()
}
