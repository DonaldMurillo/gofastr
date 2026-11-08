package mcp

import (
	"context"
	"log/slog"
	"runtime/debug"

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
	return func(ctx context.Context, params map[string]any) (any, error) {
		if err := gatedCall(gate, ctx); err != nil {
			// The gate's message is withheld by default: an unmarked
			// refusal returns the hidden-item sentinel, which tools/call
			// answers as an unknown tool (maskedRefusalRPC's rule), so a
			// Gated tool is not confirmed to exist. ShowRefusal marks the
			// refusals a caller must read — an "upgrade your plan"
			// refusal — and those cross with their own code and message
			// the same way WithToolGate refusals do.
			if refusalShown(err) {
				return nil, gateRefusal(err)
			}
			return nil, errGatedHidden
		}
		return h(ctx, params)
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
