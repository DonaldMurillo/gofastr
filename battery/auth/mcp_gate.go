package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// embedGrantRefused reports the error an MCP precondition returns for a caller
// authenticated by an embed grant.
//
// A grant resolves to the same ctx user a session does, so without this an MCP
// tool gated on MCPUser/MCPRole is reachable with a credential that lives in a
// third party's page. MCP tools run commands and read data on the caller's
// behalf; that is not a capability a surface handed to a customer's website
// should carry, and no surface declares it.
func embedGrantRefused(ctx context.Context) error {
	if _, embedded := embed.GrantFromContext(ctx); embedded {
		return errors.New("auth: this tool is not reachable from an embedded surface: an embed grant is a delegated, scoped credential, not an interactive session")
	}
	return nil
}

// MCPUser is an mcp.Gated precondition requiring an authenticated user
// on the tool call's context. Works whenever the app's session/JWT
// middleware runs globally (fwApp.Use(auth.SessionMiddleware(...))).
// The /mcp route sits on the same router, so the middleware resolves
// the caller before the tool handler runs.
//
//	app.MCP.RegisterTool("reports_rebuild", desc, schema,
//	    mcp.Gated(auth.MCPUser(), rebuildHandler))
func MCPUser() func(ctx context.Context) error {
	return func(ctx context.Context) error {
		user := GetCurrentUser(ctx)
		if user == nil {
			return errors.New("auth: this tool requires an authenticated caller: send the session cookie or Authorization header on the /mcp request")
		}
		if err := embedGrantRefused(ctx); err != nil {
			return err
		}
		if roleGateDenied(ctx, user) {
			return errors.New("auth: this tool call was refused by the access decider for the caller's roles")
		}
		return nil
	}
}

// MCPRole is an mcp.Gated precondition requiring an authenticated user
// holding ANY of the given roles, the tool-handler analogue of
// RequireRole.
//
//	app.MCP.RegisterTool("cache_flush", desc, schema,
//	    mcp.Gated(auth.MCPRole("admin"), flushHandler))
//
// Like RequireRole, the gate consults a Decider installed in the tool
// call's context (access.WithDecider / DeciderMiddleware) before the
// role check: DecisionDeny refuses, DecisionAbstain falls through.
func MCPRole(roles ...string) func(ctx context.Context) error {
	if len(roles) == 0 {
		panic("auth.MCPRole: no roles given: use auth.MCPUser() to require just authentication")
	}
	return func(ctx context.Context) error {
		user := GetCurrentUser(ctx)
		if user == nil {
			return errors.New("auth: this tool requires an authenticated caller: send the session cookie or Authorization header on the /mcp request")
		}
		if err := embedGrantRefused(ctx); err != nil {
			return err
		}
		if roleGateDenied(ctx, user) {
			return errors.New("auth: this tool call was refused by the access decider for the caller's roles")
		}
		if !hasAnyRole(user, roles) {
			return fmt.Errorf("auth: this tool requires role %v", roles)
		}
		return nil
	}
}
