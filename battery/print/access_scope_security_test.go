package print

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// gateCode runs policy on a GET carrying ctx and returns its status
// (0 = allowed).
func gateCode(policy AccessPolicy, ctx context.Context) int {
	code, _ := policy(httptest.NewRequest(http.MethodGet, "/print/doc", nil).WithContext(ctx))
	return code
}

// An API token carries its owner's user, so the default gates let a token
// minted for "customers:read" print every document its owner can. A
// scope-restricted caller now needs Scope ("print:read") on both
// built-in gates (2026-10-31 O53).
func TestPrintGatesNarrowByScope(t *testing.T) {
	user := handler.SetUser(context.Background(), struct{ ID string }{ID: "u1"})
	owner := RequireOwner(func(*http.Request, any) bool { return true })
	cases := []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"session", user, 0},
		{"token for customers:read", access.WithHeldScopes(user, []string{"customers:read"}), http.StatusForbidden},
		{"empty-scoped token", access.WithHeldScopes(user, nil), http.StatusForbidden},
		{"token for print:read", access.WithHeldScopes(user, []string{Scope}), 0},
		{"token for print:*", access.WithHeldScopes(user, []string{"print:*"}), 0},
	}
	for _, tc := range cases {
		if got := gateCode(RequireAuth, tc.ctx); got != tc.want {
			t.Errorf("SECURITY: RequireAuth, %s = %d, want %d", tc.name, got, tc.want)
		}
		if got := gateCode(owner, tc.ctx); got != tc.want {
			t.Errorf("SECURITY: RequireOwner, %s = %d, want %d", tc.name, got, tc.want)
		}
	}
}
