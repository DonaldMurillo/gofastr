package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// An embed grant's authority is SCOPED, and every scope gate in this package
// must read it that way.
//
// The embed middleware deletes Authorization and X-API-Key, so TokenMiddleware
// never runs and no token scopes are ever set on the context. Every gate built
// on TokenScopes then read "not token-authenticated" as "session — full user
// capability", and a grant minted for a read-only reporting surface passed
// RequireScope("orders:write") and RequireAPIScopes on a DELETE.
func TestGrantScopesGateLikeTokenScopes(t *testing.T) {
	readOnly := embed.Grant{Surface: "reports", Subject: "u-7", Scopes: []string{"orders:read"}}

	t.Run("HasScope reports the grant's own scopes", func(t *testing.T) {
		ctx := embed.WithGrant(t.Context(), readOnly)
		if !HasScope(ctx, "orders:read") {
			t.Fatal("HasScope denied a scope the grant carries")
		}
		if HasScope(ctx, "orders:write") {
			t.Fatal("HasScope granted orders:write to a grant scoped orders:read")
		}
		if HasScope(ctx, "admin:all") {
			t.Fatal("HasScope granted an unrelated scope to a scoped grant")
		}
	})

	t.Run("a grant with no scopes holds none", func(t *testing.T) {
		ctx := embed.WithGrant(t.Context(), embed.Grant{Surface: "pricing"})
		if HasScope(ctx, "orders:read") {
			t.Fatal("a scopeless grant was treated as unrestricted")
		}
	})

	t.Run("RequireScope refuses a grant lacking the scope", func(t *testing.T) {
		reached := false
		h := RequireScope("orders:write")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}))
		req := httptest.NewRequest(http.MethodDelete, "/api/orders/42", nil)
		req = req.WithContext(embed.WithGrant(req.Context(), readOnly))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if reached {
			t.Fatal("a read-only grant reached a write-gated handler")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("RequireAPIScopes refuses a write from a read-only grant", func(t *testing.T) {
		reached := false
		h := RequireAPIScopes("/api")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}))
		req := httptest.NewRequest(http.MethodDelete, "/api/orders/42", nil)
		req = req.WithContext(embed.WithGrant(req.Context(), readOnly))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if reached {
			t.Fatal("a read-only grant deleted through RequireAPIScopes")
		}
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("a read stays allowed", func(t *testing.T) {
		reached := false
		h := RequireAPIScopes("/api")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
		req = req.WithContext(embed.WithGrant(req.Context(), readOnly))
		h.ServeHTTP(httptest.NewRecorder(), req)

		if !reached {
			t.Fatal("a grant scoped orders:read was refused a read")
		}
	})
}

// A session is still unscoped. Sessions carry full user capability and the
// scope model is a token-level restriction only — breaking that would 403 every
// ordinary logged-in visitor.
func TestSessionsRemainUnscoped(t *testing.T) {
	if !HasScope(t.Context(), "anything:at-all") {
		t.Fatal("a request with neither token nor grant was treated as scoped")
	}
}
