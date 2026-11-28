package main

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

const maxAgentBody = 64 << 10

func mountAgentRoutes(r *router.Router, store *AdapterStore, notify func(kind, summary string)) {
	r.Get("/kiln/agent", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, agentState(store))
	}))
	r.Post("/kiln/agent", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var args struct {
			Name   string `json:"name"`
			Custom string `json:"custom,omitempty"`
		}
		req.Body = http.MaxBytesReader(w, req.Body, maxAgentBody)
		if err := handler.DecodeStrict(req.Body, &args); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		value := args.Name
		switch args.Name {
		case "", "none", "auto":
		case "custom":
			if !allowCustomAgent {
				writeJSON(w, map[string]any{
					"ok": false,
					"error": `name="custom" is disabled: it lets a request choose the command kiln executes. ` +
						`Restart with --allow-custom-agent to enable it, or use a registered adapter name.`,
				})
				return
			}
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
		if notify != nil {
			notify("agent_changed", adapter.Name)
		}
		writeJSON(w, map[string]any{
			"ok":      true,
			"current": describeAdapter(adapter),
		})
	}))

	r.Post("/kiln/agent/cancel", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancelled := store.CancelInFlight()
		if cancelled && notify != nil {
			notify("agent_turn_ended", "")
		}
		writeJSON(w, map[string]any{"ok": true, "cancelled": cancelled})
	}))
}

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
		return map[string]any{"name": "none", "display": "(no agent: chat goes to journal but nothing runs)"}
	}
	return map[string]any{"name": a.Name, "display": a.Display}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
	w.Write([]byte("\n"))
}
