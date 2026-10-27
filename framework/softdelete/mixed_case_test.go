package softdelete

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// AutoMigrate writes its DDL unquoted, so Postgres stores a declared table
// "Widgets" as widgets. SoftDelete and Restore name the declared table and
// must reach the folded one.
func TestMixedCaseSoftDeleteRestore(t *testing.T) {
	run := func(t *testing.T, db *sql.DB) {
		ent := entity.Define("Widgets", entity.EntityConfig{
			Table:  "Widgets",
			Fields: []schema.Field{{Name: "Title", Type: schema.String}},
			Scope:  &entity.ScopeConfig{SoftDelete: true},
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(db, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO Widgets (id, Title) VALUES ('w1', 'a')`); err != nil {
			t.Fatalf("seed: %v", err)
		}
		deleted := func() bool {
			t.Helper()
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM Widgets WHERE deleted_at IS NOT NULL`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n == 1
		}
		ctx := context.Background()
		if err := SoftDelete(ctx, db, "Widgets", "w1"); err != nil || !deleted() {
			t.Fatalf("SoftDelete on a mixed-case table: err=%v deleted=%v", err, deleted())
		}
		if err := Restore(ctx, db, "Widgets", "w1"); err != nil || deleted() {
			t.Fatalf("Restore on a mixed-case table: err=%v deleted=%v", err, deleted())
		}
	}
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		run(t, db)
	})
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}
