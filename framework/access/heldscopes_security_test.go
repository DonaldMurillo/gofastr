package access

import (
	"context"
	"testing"
)

// Scopes only narrow. An unscoped context (session, JWT, server code) is
// allowed; a scoped one holds exactly what its scopes grant, and an empty
// scope list grants nothing rather than reading as "no scopes installed".
func TestHeldScopesOnlyNarrow(t *testing.T) {
	ctx := context.Background()
	if !ScopeAllows(ctx, "invoices:read") {
		t.Fatal("unscoped ctx refused: scopes must only narrow")
	}
	scoped := WithHeldScopes(ctx, []string{"customers:*"})
	if !ScopeAllows(scoped, "customers:read") {
		t.Fatal("customers:* did not grant customers:read")
	}
	if ScopeAllows(scoped, "invoices:read") {
		t.Fatal("SECURITY: customers:* granted invoices:read")
	}
	empty := WithHeldScopes(ctx, nil)
	if _, ok := HeldScopes(empty); !ok {
		t.Fatal("SECURITY: an empty scope list read as unscoped")
	}
	if ScopeAllows(empty, "customers:read") {
		t.Fatal("SECURITY: an empty scope list granted a scope")
	}
}
