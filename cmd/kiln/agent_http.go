package main

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/gofastr/gofastr/core/router"
)

// mountAgentRoutes registers the runtime agent-control endpoints on r.
//
//	GET  /kiln/agent          → current adapter + available list
//	POST /kiln/agent          → switch the active adapter at runtime
//
// Used by the panel's config modal so the user can pick a harness
// (claude-code, pi, codex, custom) without restarting kiln serve. The
// adapter watcher reads the store at turn-spawn time, so any switch
// takes effect on the next chat_user event. An in-flight turn is
// cancelled when its adapter is replaced.
func mountAgentRoutes(r *router.Router, store *AdapterStore) {
	r.Get("/kiln/agent", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, agentState(store))
	}))
	r.Post("/kiln/agent", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var args struct {
			Name   string `json:"name"`             // "claude-code" | "pi" | "codex" | "auto" | "none" | "custom"
			Custom string `json:"custom,omitempty"` // freeform command if name == "custom"
		}
		if err := json.NewDecoder(req.Body).Decode(&args); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate name. Anything not in the registry / sentinel set
		// must use the explicit "custom" form to avoid silent
		// misroutes when a caller typoes an adapter name.
		value := args.Name
		switch args.Name {
		case "", "none", "auto":
			// sentinels: pass through
		case "custom":
			value = args.Custom
			if value == "" {
				writeJSON(w, map[string]any{
					"ok":    false,
					"error": `name="custom" requires a non-empty "custom" command`,
				})
				return
			}
		default:
			if _, ok := adapters[args.Name]; !ok {
				writeJSON(w, map[string]any{
					"ok":    false,
					"error": "unknown adapter name; use one of the registered adapters or name=\"custom\" with a custom command",
					"value": args.Name,
				})
				return
			}
		}

		adapter, ok := resolveAdapter(value)
		if !ok && value != "" && value != "none" {
			writeJSON(w, map[string]any{
				"ok":    false,
				"error": "adapter not resolvable (its binary isn't on PATH)",
				"value": value,
			})
			return
		}
		store.Set(adapter)
		writeJSON(w, map[string]any{
			"ok":      true,
			"current": describeAdapter(adapter),
		})
	}))
}

// agentState returns the structure consumed by both the GET endpoint
// and the panel's config modal. Each available adapter carries its
// `installed` flag so the UI can disable rows for binaries the user
// doesn't have.
func agentState(store *AdapterStore) map[string]any {
	cur := store.Get()
	available := []map[string]any{}
	names := make([]string, 0, len(adapters))
	for n := range adapters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		a := adapters[n]
		available = append(available, map[string]any{
			"name":      a.Name,
			"display":   a.Display,
			"installed": a.Detect(),
		})
	}
	return map[string]any{
		"current":   describeAdapter(cur),
		"available": available,
		"order":     adapterAutoOrder,
		"in_flight": store.InFlight(),
	}
}

func describeAdapter(a Adapter) map[string]any {
	if a.BuildArgs == nil {
		return map[string]any{"name": "none", "display": "(no agent — chat goes to journal but nothing runs)"}
	}
	return map[string]any{"name": a.Name, "display": a.Display}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
