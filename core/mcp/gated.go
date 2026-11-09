package mcp

import (
	"context"
	"log/slog"
	"reflect"
	"runtime/debug"
	"sync"
	"weak"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Gated wraps a ToolHandler with a precondition that runs before the
// handler on every call. Use it to auth-gate custom tools: the gate
// receives the tool call's context, which carries the inbound HTTP
// request (RequestFromContext) and whatever identity the router's
// middleware chain resolved onto it, so a gate can require a signed-in
// user, a role, or any other per-caller policy. A refused call answers
// exactly as an unknown tool (the gate's message is withheld unless the
// error is wrapped with ShowRefusal); the handler never runs.
//
// The entity CRUD tools don't need this. They re-dispatch through the
// router and inherit HTTP auth wholesale. Gated exists for DIRECT
// handlers: app.MCP.RegisterTool(...) registrations and
// entity.Endpoint.MCPHandler twins, which bypass the route middleware.
//
// battery/auth ships ready-made gates: auth.MCPUser() and
// auth.MCPRole("admin").
func Gated(gate func(ctx context.Context) error, h ToolHandler) ToolHandler {
	if gate == nil {
		panic("mcp.Gated: nil gate — a nil precondition would silently allow every caller")
	}
	if h == nil {
		panic("mcp.Gated: nil handler")
	}
	state := &gatedHandler{gate: gate, handler: h}
	state.wrapper = state.call
	gatedHandlers.Lock()
	// The registry must not keep discarded wrappers (and app closures)
	// alive. Prune dead entries whenever another wrapper is made.
	live := gatedHandlers.refs[:0]
	for _, ref := range gatedHandlers.refs {
		if ref.Value() != nil {
			live = append(live, ref)
		}
	}
	gatedHandlers.refs = append(live, weak.Make(state))
	gatedHandlers.Unlock()
	return state.wrapper
}

// Decorate wraps a tool handler in decorate without hiding the gates of any
// Gated wrappers h carries. A plain closure that calls a Gated handler is
// opaque to registration, so the server cannot run its gate before argument
// validation and a refused caller learns the tool's schema from the
// validation error. Decorate peels the Gated layers off h, hands the inner
// handler to decorate, and puts the same gates back outside the result, in
// the same order. The decoration then runs only for callers every gate
// allows. Use it for any wrapper placed around a ToolHandler someone else
// supplied before it is registered.
func Decorate(h ToolHandler, decorate func(ToolHandler) ToolHandler) ToolHandler {
	if h == nil {
		panic("mcp.Decorate: nil handler")
	}
	if decorate == nil {
		panic("mcp.Decorate: nil decorator")
	}
	inner, gates := gatedParts(h)
	out := decorate(inner)
	if out == nil {
		panic("mcp.Decorate: decorator returned a nil handler")
	}
	for i := len(gates) - 1; i >= 0; i-- {
		out = Gated(gates[i], out)
	}
	return out
}

// gatedHandler keeps the original function's gate available at registration
// without changing ToolHandler's public function type or executing app code.
// The wrapper retains state, so its weak registry entry lives as long as any
// caller retains the function. Its self-reference is collected with it.
type gatedHandler struct {
	gate    func(context.Context) error
	handler ToolHandler
	wrapper ToolHandler
}

var gatedHandlers struct {
	sync.Mutex
	refs []weak.Pointer[gatedHandler]
}

func (g *gatedHandler) call(ctx context.Context, params map[string]any) (any, error) {
	if err := gatedCall(g.gate, ctx); err != nil {
		if refusalShown(err) {
			return nil, gateRefusal(err)
		}
		return nil, errGatedHidden
	}
	return g.handler(ctx, params)
}

// gatedParts resolves each directly nested Gated wrapper once, at tool
// registration. Compare reflected wrapper identities, not code pointers: every
// bound call method shares code while carrying a different gate and handler.
// No callback runs under the registry lock.
func gatedParts(h ToolHandler) (ToolHandler, []func(context.Context) error) {
	gatedHandlers.Lock()
	defer gatedHandlers.Unlock()
	// This index lives only for registration. Keeping reflected wrappers
	// in the global registry would retain the closures the weak entries
	// are meant to release.
	byWrapper := make(map[reflect.Value]*gatedHandler, len(gatedHandlers.refs))
	for _, ref := range gatedHandlers.refs {
		if g := ref.Value(); g != nil {
			byWrapper[reflect.ValueOf(g.wrapper)] = g
		}
	}
	var gates []func(context.Context) error
	for {
		found := byWrapper[reflect.ValueOf(h)]
		if found == nil {
			return h, gates
		}
		gates = append(gates, found.gate)
		h = found.handler
	}
}

// gatedCall runs a Gated gate under a recover guard: a panicking gate is
// logged server-side and refused. The panic must never unwind into
// invokeHandler — its internal-error answer differs from the unknown-tool
// answer and would confirm the gated tool exists.
func gatedCall(gate func(context.Context) error, ctx context.Context) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP Gated gate recovered", "operation", "Gated gate",
				"panic", textsafe.Recovered(rec), "stack", string(debug.Stack()))
			err = errGatedHidden
		}
	}()
	return gate(ctx)
}
