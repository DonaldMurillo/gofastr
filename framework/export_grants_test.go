package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The RBAC GrantStore keeps its runtime grants and its revocation
// tombstones in two tables of its own, and ExportData left both out: a
// restore lost every grant an admin made at runtime, and every revoke of
// a permission the code declares. The restored app booted with the code
// seed and nothing subtracting it, so a revoked permission came back.
func TestExportRestoresGrantsAndRevocations(t *testing.T) {
	ctx := context.Background()
	// The code declares editor → posts:delete, as an app does at boot.
	seeded := func() *access.RolePolicy {
		p := access.NewRolePolicy()
		if err := p.Grant("editor", "posts:delete"); err != nil {
			t.Fatalf("code grant: %v", err)
		}
		return p
	}
	stores := map[*sql.DB]*access.GrantStore{}
	build := func(t *testing.T, db *sql.DB) *App {
		app := newBlogApp(t, db)
		gs := access.NewGrantStore(db, seeded())
		if err := gs.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		if err := gs.LoadInto(ctx, nil); err != nil {
			t.Fatalf("LoadInto: %v", err)
		}
		stores[db] = gs
		return app
	}
	seed := func(t *testing.T, db *sql.DB) {
		gs := stores[db]
		if err := gs.Revoke(ctx, "editor", "posts:delete"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if err := gs.Grant(ctx, "editor", "posts:publish"); err != nil {
			t.Fatalf("Grant: %v", err)
		}
	}
	forEachRestore(t, build, seed, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		// The restored app boots: a fresh policy with the code seed, loaded
		// from the restored tables.
		policy := seeded()
		if err := access.NewGrantStore(dst, policy).LoadInto(ctx, nil); err != nil {
			t.Fatalf("LoadInto after restore: %v", err)
		}
		c := access.WithRoles(access.WithPolicy(ctx, policy), []string{"editor"})
		if access.Can(c, "posts:delete") {
			t.Error("the restore brought back posts:delete, which an admin had revoked")
		}
		if !access.Can(c, "posts:publish") {
			t.Error("the restore lost posts:publish, which an admin had granted")
		}
	})
}
