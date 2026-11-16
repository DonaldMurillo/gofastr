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

// CompileActions compiles a component's action methods to JS and caches them.
// It also stores the action registry so handleServerAction can invoke Go handlers.
//
// The boot walk over registered screens calls this and records each id's
// screen, whose Policy chain handleServerAction then runs before the
// handler. An action a host compiles by calling this directly has no screen
// recorded, so no Policy runs for it: its Go handler must check the caller
// itself.
func (ds *UIHost) CompileActions(componentID string, comp component.Component) string {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if js, ok := ds.actionJS[componentID]; ok {
		return js
	}

	// If interactive, generate action registration JS
	if ic, ok := comp.(component.InteractiveComponent); ok {
		actions := component.ExtractActions(ic)
		if actions != nil {
			js := actionsToJS(componentID, actions)
			ds.actionJS[componentID] = js
			ds.actionHash[componentID] = hashStrings(js)
			ds.actionHandlers[componentID] = actions
			// Keep the component the registry came from. The embed boot walk
			// asks "is a component that ships a server action reachable from
			// an embeddable screen", and that question needs the component
			// value, not only the id it was filed under.
			ds.actionComps[componentID] = comp
			return js
		}
	}
	return ""
}

// AutoCompileActions scans all registered screens and compiles actions for
// any that implement InteractiveComponent. The component ID is derived from
// ScreenComponentID.ComponentID() if implemented, otherwise from the route path.
func (ds *UIHost) AutoCompileActions() {
	// Two routes can derive the same action id: pathToActionID turns "/"
	// into "-", so /admin/users and /admin-users both become
	// "admin-users". CompileActions caches first-wins, so the second
	// screen's registry was never stored -- its Go handlers unreachable,
	// and a data-action click on that page resolving into the OTHER
	// screen's registry and running its handler with this page's params.
	// Nothing said so at boot. Refuse instead, naming both routes: an id
	// collision is a wiring mistake with one fix (give one screen an
	// explicit ComponentID), and it cannot be diagnosed from the symptom.
	claimed := make(map[string]string, len(ds.App.Routes()))
	for _, route := range ds.App.Routes() {
		screen, ok := ds.App.Router.ScreenByPattern(route.Path)
		if !ok {
			continue
		}
		if _, ok := screen.Component.(component.InteractiveComponent); ok {
			var id string
			if cid, ok := screen.Component.(app.ScreenComponentID); ok {
				id = cid.ComponentID()
			} else {
				id = pathToActionID(route.Path)
			}
			if prev, taken := claimed[id]; taken && prev != route.Path {
				panic(fmt.Sprintf(
					"uihost: screens %q and %q both derive server-action id %q; "+
						"one screen's Go handlers would be unreachable and its data-action clicks "+
						"would run the other's. Give one of them an explicit ComponentID() "+
						"(implement app.ScreenComponentID) or rename the route.",
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

// pathToActionID derives a component action ID from a route path.
// "/" → "home", "/products" → "products", "/products/:slug" → "products-slug"
func pathToActionID(path string) string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "home"
	}
	// Replace / and : for valid JS identifiers
	name := strings.NewReplacer("/", "-", ":", "").Replace(path)
	return name
}

// GetActionJS returns all compiled action JS concatenated.
func (ds *UIHost) GetActionJS() string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	sb := borrowBuilder()
	defer returnBuilder(sb)
	// Sorted so the emitted script (and everything downstream: screen
	// cache keys, gzip dedup, hydration diffs) is byte-stable across
	// renders and replicas.
	for _, name := range slices.Sorted(maps.Keys(ds.actionJS)) {
		sb.WriteString(ds.actionJS[name])
		sb.WriteString("\n")
	}
	return sb.String()
}

// HasActions reports whether any component has compiled action JS. Use this
// instead of GetActionJS() != "": the latter builds the entire concatenated
// bundle just to test for any entries, which is pure waste on every page render.
func (ds *UIHost) HasActions() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.actionJS) > 0
}

// actionScriptTags returns one content-addressed <script> tag per
// compiled action registry whose component id appears in the rendered
// page (data-component/data-widget). Sorted for deterministic output.
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

// handleActionsJS serves all compiled action JS. The output enumerates
// every registered server-action and component id on the host, useful
// surface for an attacker mapping the app, so we require a session or an
// embed grant before serving it. The runtime fetches this script after
// first paint from a same-origin page, by which point the session cookie
// has been minted; inside a frame there is no cookie and the grant is
// what the caller has.
//
// A frame that cannot fetch this script has no action handlers at all:
// nothing else calls __gofastr.register, so every data-action click is
// preventDefault()ed and then dropped, and every data-action-mount node
// stays empty.
// Live pages stopped referencing this concat in favor of per-id scripts
// (actionScriptTags + the actionloader module); the endpoint remains for
// export parity and external callers, now with the shared validator
// policy instead of no cache header at all.
func (ds *UIHost) handleActionsJS(w http.ResponseWriter, r *http.Request) {
	if !ds.requireSessionOrEmbedGrant(w, r) {
		return
	}
	js := ds.GetActionJS()
	serveVersionedText(w, r, "application/javascript; charset=utf-8", js, hashStrings(js), true)
}

// handleServerAction receives a server action invocation from the client.
// The client POSTs the action name, component ID, and parameters;
// the server looks up the registered Go handler, invokes it, and
// responds with a JSON result.
func (ds *UIHost) handleServerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
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
		Action string            `json:"action"`
		Params map[string]string `json:"params"`
		// Session is read by nothing: the session is the HttpOnly cookie
		// (requireValidSession), which script cannot see. The runtime
		// stopped sending it; the field stays so a tab still holding an
		// older runtime.js decodes under the strict decoder instead of
		// failing every server action until it reloads.
		Session     string `json:"session"`
		ComponentID string `json:"componentId"`
		Page        string `json:"page"`
	}
	if !decodeBounded(w, r, &body) {
		return
	}

	componentID := body.ComponentID
	actionName := body.Action

	// Look up the action registry for this component. Return 404 for
	// unknown component/action so probing reveals only what already-
	// rendered HTML reveals.
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

	// Auth required to actually invoke a known action.
	//
	// A session, and deliberately NOT an embed grant. The action registry is
	// app-global and keyed by (componentID, action) with no relationship to any
	// surface, so accepting a grant here would let a credential minted for one
	// surface invoke any action registered anywhere in the app, including from
	// a surface with no subject at all, which is the shape a public pricing
	// table takes. There is no surface a component belongs to for this code to
	// check against, and the embed runtime publishes every compiled action name,
	// so obscurity constrains nothing either.
	//
	// The consequence is that a component whose client handler calls
	// G.serverAction does not work inside a frame. That was already true before
	// embeds existed and is not a regression; what IS fixed is the separate bug
	// that left the client half dead too (see handleEmbedRuntimeJS). Making this
	// work needs the app to say which surfaces may invoke which actions, which
	// is an API decision, not something to infer here.
	//
	// Read the guarantee narrowly. A uihost session is SELF-MINTED: any
	// same-origin caller can POST /__gofastr/session and get one with no
	// credential, because island state needs an id before any user exists. So
	// this check does not make server actions privileged. The owning screen's
	// Policy chain runs next, which keeps a caller the page refuses away from
	// its actions; anything finer than that page-level gate (per record, per
	// param) the handler must still check itself. What the session check
	// does buy is that a grant confers nothing here that an anonymous
	// visitor did not already have, which is exactly what
	// handleCreateSession's matching refusal preserves.
	if _, ok := ds.requireValidSession(w, r); !ok {
		return
	}

	// The owning screen's Policy chain. The page render runs it before Load,
	// and the actions are compiled from that same screen's component, so a
	// caller the policy keeps off the page must not run them either. The
	// runtime sends the page path the action came from; when it resolves to
	// this screen, the policy sees that page's route params, and so decides
	// what a render of that URL would. A path the client picks gains nothing
	// a page load would not: it names a page the policy then judges. Any
	// other path leaves the params empty, and a policy that reads one
	// refuses. A redirect decision is a refusal here, since an RPC cannot
	// follow it. The whole policy phase runs, region guards included: an
	// outlet guard that sends the visitor to /login keeps them off the
	// page as surely as the screen chain does. With no page of this screen
	// named, PatternPolicy decides, and a dynamic screen carrying a region
	// guard or a Requires resolver refuses, since neither has a URL to
	// judge.
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
					// The page answers an error, never itself: refuse.
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

	// Invoke the Go handler if one exists
	if actionDef.Handler != nil {
		ctx := component.NewComponentContextFor(r.Context(), actionName, "", body.Params)
		actionDef.Handler(ctx)

		w.Header().Set("Content-Type", "application/json")
		// The message is in the caller's locale.
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"action":  actionName,
			"message": reasonActionProcessed.Text(r.Context()),
		})
		return
	}

	// No Go handler; just acknowledge
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"action":  actionName,
		"message": reasonActionAcknowledged.Text(r.Context()),
	})
}

// handleWidgetJS serves compiled JavaScript for a specific widget:
// the SSR path emits one tag per on-page component and the actionloader
// module fetches the rest on navigation; lazy hydration keeps using it
// too. Session-gated like the old whole-app concat was (this per-id
// endpoint used to be LESS gated than the bundle it replaces), and
// served private, no-cache with its content hash as the revalidation ETag.
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

// actionsToJS converts an ActionRegistry to browser-runnable JavaScript
// using the ClientJS field from each ActionDef. Each action's ClientJS
// is wrapped in a handler function and registered with the runtime.
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

		// Inject componentId into serverAction calls so the server can route
		// to the correct action handler.
		body = strings.ReplaceAll(body, "G.serverAction(", fmt.Sprintf("G._serverActionFor(%q, ", componentID))

		sb.WriteString(fmt.Sprintf("    %q: (params) => {\n      %s\n    }", action.Event, body))
	}

	sb.WriteString("\n  };\n")
	sb.WriteString("  G.register(id, handlers);\n")
	sb.WriteString("})();\n")

	return sb.String()
}
