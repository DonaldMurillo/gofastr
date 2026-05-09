package chat

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gofastr/gofastr/core/router"
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
}

// New constructs a chat Server.
func New(l *live.Live, t *protocol.Tools) *Server {
	return &Server{live: l, tools: t}
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
	res, err := s.dispatch(r.Context(), name, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeResult(w, res)
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
