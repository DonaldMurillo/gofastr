package access_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestGrantStorePureSQLiteLifecycle(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	policy := access.NewRolePolicy()
	store := access.NewGrantStore(db, policy)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	if err := store.Grant(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("first Grant: %v", err)
	}
	if err := store.Grant(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("idempotent Grant: %v", err)
	}
	fresh := access.NewRolePolicy()
	if err := access.NewGrantStore(db, fresh).LoadInto(ctx, fresh); err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	if roles := fresh.Roles(); len(roles) != 1 || roles[0] != "editor" {
		t.Fatalf("roles = %v, want [editor]", roles)
	}
	if err := store.Revoke(ctx, "editor", "posts:read"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	reloaded := access.NewRolePolicy()
	if err := access.NewGrantStore(db, reloaded).LoadInto(ctx, reloaded); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if perms := reloaded.PermissionsOf("editor"); len(perms) != 0 {
		t.Fatalf("permissions after revoke = %v, want none", perms)
	}
}
