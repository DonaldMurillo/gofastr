package framework

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// With WithDebugAuthorize installed, the custom predicate decided alone:
// an API token an operator minted for "customers:read" read the pid,
// memory layout and goroutine stacks whenever the predicate admitted its
// owner. A scope-restricted caller must also hold access.RoleScope("admin")
// before the predicate runs, as the default role gate already requires.
func TestDebugAuthorizeNarrowsByScope(t *testing.T) {
	app := NewApp(WithDebugAuthorize(func(ctx context.Context) bool {
		u, _ := handler.GetUser(ctx)
		return u == "ops"
	}))
	app.registerDebugEndpoints()
	ops := handler.SetUser(context.Background(), "ops")
	cases := []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"session", ops, http.StatusOK},
		{"token for customers:read", access.WithHeldScopes(ops, []string{"customers:read"}), http.StatusForbidden},
		{"empty-scoped token", access.WithHeldScopes(ops, nil), http.StatusForbidden},
		{"token for role:ops", access.WithHeldScopes(ops, []string{"role:ops"}), http.StatusForbidden},
		{"token for role:admin", access.WithHeldScopes(ops, []string{"role:admin"}), http.StatusOK},
		{"token for *:*", access.WithHeldScopes(ops, []string{"*:*"}), http.StatusOK},
	}
	for _, tc := range cases {
		for _, path := range []string{"/.debug/stats", "/.debug/goroutineleak"} {
			if got := debugStatus(app, path, tc.ctx); got != tc.want {
				t.Errorf("SECURITY: %s: %s = %d, want %d", tc.name, path, got, tc.want)
			}
		}
	}
}
