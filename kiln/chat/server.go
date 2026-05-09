package chat

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
)

//go:embed assets/host.html
var hostHTML string

//go:embed assets/widget.js
var widgetJS string

// WidgetTag is the HTML snippet to embed the floating widget on a page.
// kiln/render auto-injects this on every page it serves; user-built
// apps can drop it in manually. The widget remembers open/closed state
// in localStorage so navigating between pages preserves the user's
// preference.
const WidgetTag = `<script src="/kiln/chat/widget.js" data-corner="bottom-right"></script>`

// Server hosts the in-app Kiln surfaces: the floating widget assets,
// the world-state JSON endpoint, the tool dispatcher, and the SSE bus.
type Server struct {
	live  *live.Live
	tools *protocol.Tools

	// callCounter is a monotonic ID source for tool_call envelopes
	// journaled by the HTTP dispatcher. Atomic so concurrent HTTP
	// requests can each get a unique id without locking.
	callCounter int64
}

// New constructs a chat Server.
func New(l *live.Live, t *protocol.Tools) *Server {
	return &Server{live: l, tools: t}
}

// journaledTools is the set of tools whose calls/results we wrap in
// tool_call/tool_result journal entries for observability. We skip
// read-only ops (world_get) and ops whose own kind already journals
// the meaningful state (chat, propose_plan, approve_plan, reject_plan).
var journaledTools = map[string]bool{
	"set_app_config": true,
	"add_entity":     true,
	"update_entity":  true,
	"delete_entity":  true,
	"add_field":      true,
	"delete_field":   true,
	"add_page":       true,
	"delete_page":    true,
	"add_hook":       true,
	"delete_hook":    true,
	"add_route":      true,
	"delete_route":   true,
	"add_seed":       true,
	"undo":           true,
}

// Mount registers the panel routes onto r. The host fallback page is
// NOT mounted here — kiln/render installs it as the rebuilt app's
// NotFound handler so every URL not otherwise claimed shows the widget.
func (s *Server) Mount(r *router.Router) {
	r.Get("/kiln/chat/widget.js", http.HandlerFunc(s.serveWidgetJS))
	r.Get("/kiln/chat/widget.css", http.HandlerFunc(s.serveWidgetCSS))
	r.Get("/kiln/chat/base.css", http.HandlerFunc(s.serveBaseCSS))
	r.Get("/kiln/world", http.HandlerFunc(s.serveWorld))
	r.Post("/kiln/chat/message", http.HandlerFunc(s.serveChatMessage))
	r.Post("/kiln/tool/{name}", http.HandlerFunc(s.serveToolDispatch))
	r.Get("/.kiln/events", http.HandlerFunc(s.live.ServeSSE))
}

// HostHTML is the empty-state shell. Returned to any unmatched HTML
// request so the floating widget is always reachable.
func HostHTML() string { return hostHTML }

func (s *Server) serveWidgetJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, widgetJS)
}

func (s *Server) serveWidgetCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, widgetCSS())
}

func (s *Server) serveBaseCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, baseCSS())
}

func (s *Server) serveWorld(w http.ResponseWriter, _ *http.Request) {
	sess := s.live.Session()
	resp := map[string]any{
		"world": sess.World,
		"session": map[string]any{
			"chat":  sess.Chat,
			"plans": sess.Plans,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) serveChatMessage(w http.ResponseWriter, r *http.Request) {
	var args protocol.ChatArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	res := s.tools.Chat(r.Context(), args)
	writeResult(w, res)
}

func (s *Server) serveToolDispatch(w http.ResponseWriter, r *http.Request) {
	name := router.Param(r, "name")
	if name == "" {
		http.Error(w, "missing tool name", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Journal a tool_call envelope BEFORE dispatching, so the panel
	// renders the agent's intent even if the underlying tool fails. The
	// ID flows through to the matching tool_result so the widget can
	// pair them.
	var callID string
	if journaledTools[name] {
		callID = s.nextCallID()
		var args map[string]any
		if len(body) > 0 {
			_ = json.Unmarshal(body, &args)
		}
		_ = s.applyEntry(journal.KindToolCall, journal.ToolCallPayload{
			CallID: callID,
			Name:   name,
			Args:   args,
		})
	}

	res, err := s.dispatch(r.Context(), name, bytes.NewReader(body))
	if err != nil {
		// Journal a synthetic tool_result with the parse error so the
		// agent's failed call still appears in the timeline.
		if callID != "" {
			_ = s.applyEntry(journal.KindToolResult, journal.ToolResultPayload{
				CallID: callID,
				OK:     false,
				Error:  err.Error(),
				Kind:   "validation",
			})
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if callID != "" {
		_ = s.applyEntry(journal.KindToolResult, journal.ToolResultPayload{
			CallID: callID,
			OK:     res.OK,
			Result: res.Result,
			Error:  res.Error,
			Kind:   res.Kind,
			Hint:   res.Hint,
		})
	}
	writeResult(w, res)
}

func (s *Server) nextCallID() string {
	n := atomic.AddInt64(&s.callCounter, 1)
	return fmt.Sprintf("c%d-%d", time.Now().UnixNano(), n)
}

// applyEntry builds a journal Entry for the given kind/payload and feeds
// it through the live mutator. The chat server uses this only for
// envelope kinds (KindToolCall, KindToolResult) — the underlying tool
// dispatch journals world_edit / plan_* entries through protocol.Tools.
func (s *Server) applyEntry(kind journal.Kind, payload any) error {
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddInt64(&s.callCounter, 1))
	entry, err := journal.NewEntry(id, time.Now().UTC(), kind, "", payload)
	if err != nil {
		return err
	}
	return s.live.Apply(entry)
}

func writeResult(w http.ResponseWriter, res protocol.Result) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// dispatch is the bridge from JSON tool calls to typed Tools methods.
func (s *Server) dispatch(ctx context.Context, name string, body interface {
	Read(p []byte) (n int, err error)
}) (protocol.Result, error) {
	dec := json.NewDecoder(body)
	switch name {
	case "world_get":
		var args protocol.WorldGetArgs
		if err := dec.Decode(&args); err != nil && err.Error() != "EOF" {
			return protocol.Result{}, err
		}
		return s.tools.WorldGet(ctx, args), nil
	case "set_app_config":
		var args protocol.SetAppConfigArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.SetAppConfig(ctx, args), nil
	case "add_entity":
		var args protocol.AddEntityArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddEntity(ctx, args), nil
	case "update_entity":
		var args protocol.UpdateEntityArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.UpdateEntity(ctx, args), nil
	case "delete_entity":
		var args protocol.DeleteEntityArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.DeleteEntity(ctx, args), nil
	case "add_field":
		var args protocol.AddFieldArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddField(ctx, args), nil
	case "delete_field":
		var args protocol.DeleteFieldArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.DeleteField(ctx, args), nil
	case "add_page":
		var args protocol.AddPageArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddPage(ctx, args), nil
	case "delete_page":
		var args protocol.DeletePageArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.DeletePage(ctx, args), nil
	case "add_hook":
		var args protocol.AddHookArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddHook(ctx, args), nil
	case "delete_hook":
		var args protocol.DeleteHookArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.DeleteHook(ctx, args), nil
	case "add_route":
		var args protocol.AddRouteArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddRoute(ctx, args), nil
	case "delete_route":
		var args protocol.DeleteRouteArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.DeleteRoute(ctx, args), nil
	case "add_seed":
		var args protocol.AddSeedArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.AddSeed(ctx, args), nil
	case "propose_plan":
		var args protocol.ProposePlanArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.ProposePlan(ctx, args), nil
	case "approve_plan":
		var args protocol.ApprovePlanArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.ApprovePlan(ctx, args), nil
	case "reject_plan":
		var args protocol.RejectPlanArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.RejectPlan(ctx, args), nil
	case "undo":
		return s.tools.Undo(ctx, protocol.UndoArgs{}), nil
	case "chat":
		var args protocol.ChatArgs
		if err := dec.Decode(&args); err != nil {
			return protocol.Result{}, err
		}
		return s.tools.Chat(ctx, args), nil
	}
	return protocol.Result{}, fmt.Errorf("unknown tool %q", name)
}
