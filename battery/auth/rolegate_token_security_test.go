package auth

// A scoped API token carries its owner's roles, so without a role scope a
// token an admin minted for "customers:read" passed RequireRole("admin") and
// MCPRole("admin"). The role gates count a role for a scoped caller only when
// it holds access.RoleScope of that role, the rule framework.MCPRequireRole
// already follows (issue #614, O51).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func adminCtx(scopes []string) context.Context {
	user := &BasicUser{ID: "u1", Email: "u1@example.com", Roles: []string{"admin"}}
	ctx := handler.SetUser(context.Background(), user)
	if scopes != nil {
		ctx = WithTokenScopes(ctx, scopes)
	}
	return ctx
}

func requireRoleCode(ctx context.Context) int {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/admin", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	RequireRole("admin")(inner).ServeHTTP(rec, req)
	return rec.Code
}

func TestRoleGateTokenNeedsRoleScope(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		allow  bool
	}{
		{"session", nil, true},
		{"unrelated scope", []string{"customers:read"}, false},
		{"empty scopes", []string{}, false},
		{"role scope", []string{"customers:read", "role:admin"}, true},
		{"role wildcard", []string{"role:*"}, true},
		{"other role", []string{"role:editor"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := adminCtx(tc.scopes)
			code := requireRoleCode(ctx)
			if tc.allow && code != http.StatusOK {
				t.Errorf("RequireRole: got %d, want 200", code)
			}
			if !tc.allow && code != http.StatusForbidden {
				t.Errorf("RequireRole: got %d, want 403", code)
			}
			err := MCPRole("admin")(ctx)
			if tc.allow && err != nil {
				t.Errorf("MCPRole: refused: %v", err)
			}
			if !tc.allow && err == nil {
				t.Error("MCPRole: allowed a token with no role:admin scope")
			}
		})
	}
}
