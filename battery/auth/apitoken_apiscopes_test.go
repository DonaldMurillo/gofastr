package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// driveAPIScopes runs one request through RequireAPIScopes with the given
// token scopes on ctx (nil scopes = session/JWT caller) and returns the
// status the client saw.
func driveAPIScopes(t *testing.T, prefix, method, path string, scopes []string) int {
	t.Helper()
	h := RequireAPIScopes(prefix)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, path, nil)
	if scopes != nil {
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, scopes))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// The scope is derived from the route: first segment after the prefix is
// the resource, GET/HEAD map to :read, everything else to :write.
func TestAPIScopes_DerivedFromRoute(t *testing.T) {
	cases := []struct {
		method, path string
		scopes       []string
		want         int
	}{
		{"GET", "/api/customers", []string{"customers:read"}, 200},
		{"GET", "/api/customers/42", []string{"customers:*"}, 200},
		{"GET", "/api/customers/_events", []string{"customers:read"}, 200},
		{"POST", "/api/customers", []string{"customers:read"}, 403},
		{"POST", "/api/customers", []string{"customers:write"}, 200},
		{"PATCH", "/api/customers/_batch", []string{"customers:write"}, 200},
		{"DELETE", "/api/customers/42", []string{"*:*"}, 200},
		{"GET", "/api/invoices", []string{"customers:*"}, 403},
		{"POST", "/api/invoices", []string{}, 403}, // empty scopes grant nothing
	}
	for _, c := range cases {
		if got := driveAPIScopes(t, "/api", c.method, c.path, c.scopes); got != c.want {
			t.Errorf("%s %s scopes=%v = %d, want %d", c.method, c.path, c.scopes, got, c.want)
		}
	}
}

// Session/JWT callers (no token scopes on ctx) and paths outside the prefix
// pass through untouched.
func TestAPIScopes_SessionAndOffPrefixPass(t *testing.T) {
	if got := driveAPIScopes(t, "/api", "DELETE", "/api/customers/42", nil); got != 200 {
		t.Errorf("session caller = %d, want 200", got)
	}
	if got := driveAPIScopes(t, "/api", "GET", "/login", []string{}); got != 200 {
		t.Errorf("off-prefix path = %d, want 200", got)
	}
	if got := driveAPIScopes(t, "/api", "GET", "/apiary/things", []string{}); got != 200 {
		t.Errorf("prefix must match on a segment boundary, got %d", got)
	}
}
