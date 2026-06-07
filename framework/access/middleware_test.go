package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCan_NoPolicyDenies confirms the package-level Can fails closed when no
// policy is installed — the secure default for an un-wired request.
func TestCan_NoPolicyDenies(t *testing.T) {
	if Can(context.Background(), "posts:read") {
		t.Fatal("Can returned true with no policy in context")
	}
}

// TestMiddleware_InstallsPolicyAndRoles verifies access.Middleware wires the
// policy + roles so a downstream Can resolves the caller's permissions.
func TestMiddleware_InstallsPolicyAndRoles(t *testing.T) {
	policy := NewRolePolicy()
	policy.Grant("editor", "posts:write")

	var sawWrite, sawDelete bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawWrite = Can(r.Context(), "posts:write")
		sawDelete = Can(r.Context(), "posts:delete")
	})
	mw := Middleware(policy, func(ctx context.Context) []string { return []string{"editor"} })

	req := httptest.NewRequest(http.MethodGet, "/posts", nil)
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	if !sawWrite {
		t.Error("editor should have posts:write")
	}
	if sawDelete {
		t.Error("editor should NOT have posts:delete")
	}
}
