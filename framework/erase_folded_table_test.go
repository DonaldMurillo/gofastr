package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// The auth stores create a configured "Members" as the folded members, as
// AutoMigrate does. The SQLite existence probe compared sqlite_master's
// name with case, so the eraser registered under "Members" was skipped and
// the erasure reported success with the account still there.
func TestEraseFindsFoldedTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE members (id TEXT PRIMARY KEY, email TEXT)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO members (id, email) VALUES ('u1', 'a@x.test'), ('u2', 'b@x.test')`); err != nil {
			t.Fatal(err)
		}
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "fold_members", Source: "test", Table: "Members",
			Column: "id", Mode: datexport.EraseDelete,
		})
		t.Cleanup(func() { datexport.UnregisterEraser("fold_members") })
		app := NewApp(WithDB(db))
		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM members WHERE id = 'u1'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("erased account rows = %d (%v), want 0", n, err)
		}
	})
}
