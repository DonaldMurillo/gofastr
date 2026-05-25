package crud

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// TestCrudAPI_GetOneScopesByOwner pins the programmatic in-process
// equivalent of the HTTP-handler RequireOwner fix. CrudHandler.GetOne /
// ListAll / CountAll are the helpers typed repositories + hooks call
// from inside request handlers — they MUST mirror the HTTP path's
// owner scoping or every in-process caller becomes a cross-user leak
// vector.
func TestCrudAPI_GetOneScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")
	seedRow(t, db, "log-b1", "bob", "bob secret")

	// Bob calls GetOne for alice's row id. Without the fix the call
	// returns alice's row (programmatic bypass).
	ctx := ctxWithUser("bob")
	got, err := ch.GetOne(ctx, "log-a1", nil)
	if err == nil {
		if got["user_id"] == "alice" {
			t.Errorf("PROGRAMMATIC BYPASS: bob.GetOne(alice's id) returned alice's row: %+v", got)
		}
	}
	// Acceptable outcomes: errNotFound or err containing "not found".
	if err == nil {
		t.Errorf("GetOne should error for cross-user lookup, got row: %+v", got)
	} else if !strings.Contains(err.Error(), "not found") {
		t.Logf("note: cross-user GetOne returned %v (want some not-found signal)", err)
	}
}

func TestCrudAPI_ListAllScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice row")
	seedRow(t, db, "log-b1", "bob", "bob row")

	ctx := ctxWithUser("alice")
	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAll len = %d, want 1 (alice's rows only). got: %+v", len(rows), rows)
	}
	if rows[0]["user_id"] != "alice" {
		t.Errorf("PROGRAMMATIC LEAK: alice's ListAll returned bob's row: %+v", rows[0])
	}
}

func TestCrudAPI_CountAllScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "x")
	seedRow(t, db, "log-a2", "alice", "y")
	seedRow(t, db, "log-b1", "bob", "z")

	ctx := ctxWithUser("alice")
	n, err := ch.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("CountAll = %d, want 2 (alice's rows only)", n)
	}
}

// ctxWithUser is the context-only mirror of withTestUser in owner_test.go.
func ctxWithUser(uid string) context.Context {
	return handler.SetUser(context.Background(), &testUser{id: uid})
}
