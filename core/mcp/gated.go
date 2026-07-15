package mcp

import "context"

// Gated wraps a ToolHandler with a precondition that runs before the
// handler on every call. Use it to auth-gate custom tools: the gate
// receives the tool call's context, which carries the inbound HTTP
// request (RequestFromContext) and whatever identity the router's
// middleware chain resolved onto it — so a gate can require a signed-in
// user, a role, or any other per-caller policy. A refused call returns
// the gate's error as the JSON-RPC tool error; the handler never runs.
//
// The entity CRUD tools don't need this — they re-dispatch through the
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
		if err := gate(ctx); err != nil {
			return nil, err
		}
		return h(ctx, params)
	}
}
