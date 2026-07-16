package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TokenMiddleware must expose the authenticating token's own ID in ctx via
// TokenID. Owner + scopes alone can't attribute a request to a SPECIFIC
// credential — one owner can hold many tokens, and per-token metering,
// quotas, and audit trails all need to know which one was used.
func TestTokenMiddleware_ExposesTokenID(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()
	alice := &BasicUser{ID: "alice", Email: "alice@example.com", Roles: []string{"user"}}
	users := &staticUserStore{byID: map[string]User{"alice": alice}}

	pt, issued, err := IssueToken(ctx, ts, TokenSpec{Name: "meter-me", OwnerKind: "user", OwnerID: "alice"})
	if err != nil {
		t.Fatal(err)
	}

	var gotID string
	var gotOK bool
	h := TokenMiddleware(users, nil, ts)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotID, gotOK = TokenID(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), bearerRequest("GET", "/x", pt, ""))

	if !gotOK || gotID != issued.ID {
		t.Fatalf("TokenID(ctx) = (%q, %v), want (%q, true)", gotID, gotOK, issued.ID)
	}

	// Session/JWT requests carry no token identity.
	if id, ok := TokenID(context.Background()); ok || id != "" {
		t.Fatalf("TokenID on a non-token ctx = (%q, %v), want empty", id, ok)
	}
}

// ListAll is the admin view: every token across owners, newest first.
// RevokeAny is the admin revoke: ignores owner scoping, idempotent,
// ErrTokenNotFound on unknown ids. Both live on the concrete SQL store only —
// the plugin's self-service routes must never reach them.
func TestSQLAPITokenStore_AdminListAndRevoke(t *testing.T) {
	_, ts, _ := newTokenTestDB(t)
	ctx := context.Background()

	_, ta, err := IssueToken(ctx, ts, TokenSpec{Name: "a", OwnerKind: "user", OwnerID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_, tb, err := IssueToken(ctx, ts, TokenSpec{Name: "b", OwnerKind: "user", OwnerID: "bob"})
	if err != nil {
		t.Fatal(err)
	}

	all, err := ts.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll: want 2 tokens across owners, got %d", len(all))
	}
	owners := map[string]bool{}
	for _, tok := range all {
		owners[tok.OwnerID] = true
		if tok.Prefix == "" {
			t.Errorf("ListAll token %s missing prefix", tok.ID)
		}
	}
	if !owners["alice"] || !owners["bob"] {
		t.Errorf("ListAll must span owners, got %v", owners)
	}

	// Admin revoke crosses owner boundaries (owner-scoped Revoke would 404).
	if err := ts.RevokeAny(ctx, tb.ID); err != nil {
		t.Fatalf("RevokeAny: %v", err)
	}
	all, err = ts.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range all {
		if tok.ID == tb.ID && tok.RevokedAt == nil {
			t.Error("RevokeAny did not stamp RevokedAt")
		}
		if tok.ID == ta.ID && tok.RevokedAt != nil {
			t.Error("RevokeAny revoked the wrong token")
		}
	}

	// Idempotent + unknown id contract.
	if err := ts.RevokeAny(ctx, tb.ID); err != nil {
		t.Errorf("RevokeAny must be idempotent, got %v", err)
	}
	if err := ts.RevokeAny(ctx, "no-such-id"); err != ErrTokenNotFound {
		t.Errorf("RevokeAny unknown id: want ErrTokenNotFound, got %v", err)
	}
}
