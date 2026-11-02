package access_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// Revoke wrote its two tables one statement at a time on the pool. A
// failure between them (here the tombstone table is gone) returned an
// error after the grant row was already deleted: the live policy still
// held the permission, no tombstone recorded the revoke, and no peer was
// told. A failed Revoke now leaves the database as it found it.
func TestGrantStoreFailedWriteLeavesTablesUnchanged(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *sql.DB{
		"sqlite":   newGrantDB,
		"postgres": pgtest.DB,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			store := access.NewGrantStore(db, access.NewRolePolicy())
			if err := store.EnsureSchema(ctx); err != nil {
				t.Fatalf("EnsureSchema: %v", err)
			}
			if err := store.Grant(ctx, "editor", "posts:write"); err != nil {
				t.Fatalf("Grant: %v", err)
			}
			if _, err := db.ExecContext(ctx, `DROP TABLE access_grants_revoked`); err != nil {
				t.Fatalf("drop tombstones: %v", err)
			}
			if err := store.Revoke(ctx, "editor", "posts:write"); err == nil {
				t.Fatal("Revoke succeeded with no tombstone table")
			}
			if got := grantRows(t, db, "editor"); len(got) != 1 {
				t.Fatalf("a failed Revoke deleted the grant row: rows = %v", got)
			}
		})
	}
}
