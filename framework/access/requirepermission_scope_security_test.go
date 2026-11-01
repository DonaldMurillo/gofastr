package access_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// A scoped credential (an API token, an embed grant) carries its owner's
// roles, so a role policy alone would let a token minted for one resource
// pass a permission gate on another. RequirePermission must also ask the
// scope model: the token needs a scope naming the permission. Unscoped
// callers (sessions, JWTs) are unaffected, and a Decider's Allow does not
// lift the narrowing.
func TestRequirePermissionNarrowsByScope(t *testing.T) {
	t.Parallel()
	policy := access.NewRolePolicy()
	if err := policy.Grant("admin", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	base := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"admin"})
	allowAll := access.WithDecider(base, func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionAllow
	})
	gate := access.RequirePermission("posts:write")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	code := func(ctx context.Context) int {
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx))
		return rec.Code
	}

	cases := []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"session admin", base, http.StatusOK},
		{"token without scope", access.WithHeldScopes(base, []string{"customers:read"}), http.StatusForbidden},
		{"token, decider allows", access.WithHeldScopes(allowAll, []string{"customers:read"}), http.StatusForbidden},
		{"empty-scoped token", access.WithHeldScopes(base, nil), http.StatusForbidden},
		{"token with posts:write", access.WithHeldScopes(base, []string{"posts:write"}), http.StatusOK},
		{"token with posts:*", access.WithHeldScopes(base, []string{"posts:*"}), http.StatusOK},
	}
	for _, tc := range cases {
		if got := code(tc.ctx); got != tc.want {
			t.Errorf("SECURITY: %s: RequirePermission(posts:write) = %d, want %d", tc.name, got, tc.want)
		}
	}
}
