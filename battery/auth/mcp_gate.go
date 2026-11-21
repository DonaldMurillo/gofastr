package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/embed"
)

func embedGrantRefused(ctx context.Context) error {
	if _, embedded := embed.GrantFromContext(ctx); embedded {
		return errors.New("auth: this tool is not reachable from an embedded surface: an embed grant is a delegated, scoped credential, not an interactive session")
	}
	return nil
}

// MCPUser is an mcp.Gated precondition requiring a signed-in caller that is no embed grant.
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

// MCPRole is an mcp.Gated precondition requiring a caller holding one of roles, as RequireRole judges it.
func MCPRole(roles ...string) func(ctx context.Context) error {
	if len(roles) == 0 {
		panic("auth.MCPRole: no roles given: use auth.MCPUser() to require just authentication")
	}
	requireUser := MCPUser()
	return func(ctx context.Context) error {
		if err := requireUser(ctx); err != nil {
			return err
		}
		if !hasAnyRole(ctx, GetCurrentUser(ctx), roles) {
			return fmt.Errorf("auth: this tool requires role %v", roles)
		}
		return nil
	}
}
