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

// Gated wraps h with a per-caller gate run before it on every call; a refusal answers as an unknown tool unless wrapped with ShowRefusal. It panics on a nil gate or handler.
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

// Decorate wraps h in decorate while keeping h's Gated layers outside the result, in the same order, so the server still runs those gates first.
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

func gatedParts(h ToolHandler) (ToolHandler, []func(context.Context) error) {
	gatedHandlers.Lock()
	defer gatedHandlers.Unlock()
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
