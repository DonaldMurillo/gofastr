package auth

import (
	"context"
	"errors"
	"fmt"
)

// MCPUser is an mcp.Gated precondition requiring an authenticated user
// on the tool call's context. Works whenever the app's session/JWT
// middleware runs globally (fwApp.Use(auth.SessionMiddleware(...)))
// — the /mcp route sits on the same router, so the middleware resolves
// the caller before the tool handler runs.
//
//	app.MCP.RegisterTool("reports_rebuild", desc, schema,
//	    mcp.Gated(auth.MCPUser(), rebuildHandler))
func MCPUser() func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if GetCurrentUser(ctx) == nil {
			return errors.New("auth: this tool requires an authenticated caller — send the session cookie or Authorization header on the /mcp request")
		}
		return nil
	}
}

// MCPRole is an mcp.Gated precondition requiring an authenticated user
// holding ANY of the given roles — the tool-handler analogue of
// RequireRole.
//
//	app.MCP.RegisterTool("cache_flush", desc, schema,
//	    mcp.Gated(auth.MCPRole("admin"), flushHandler))
func MCPRole(roles ...string) func(ctx context.Context) error {
	if len(roles) == 0 {
		panic("auth.MCPRole: no roles given — use auth.MCPUser() to require just authentication")
	}
	return func(ctx context.Context) error {
		user := GetCurrentUser(ctx)
		if user == nil {
			return errors.New("auth: this tool requires an authenticated caller — send the session cookie or Authorization header on the /mcp request")
		}
		if !hasAnyRole(user, roles) {
			return fmt.Errorf("auth: this tool requires role %v", roles)
		}
		return nil
	}
}
