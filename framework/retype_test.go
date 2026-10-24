package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A text column declared as an integer converts on apply: Postgres has no
// assignment cast from text, so the change carries its own.
func TestRetypeTextToInteger(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if _, err := db.Exec(`CREATE TABLE tallies (id TEXT PRIMARY KEY, n TEXT)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO tallies (id, n) VALUES ('a', '7')`); err != nil {
			t.Fatal(err)
		}
		reg := NewRegistry()
		if err := reg.Register(&entity.Entity{Config: entity.EntityConfig{
			Name:       "tallies",
			Table:      "tallies",
			Fields:     []schema.Field{{Name: "id", Type: schema.String}, {Name: "n", Type: schema.Int}},
			PrimaryKey: []string{"id"},
		}}); err != nil {
			t.Fatal(err)
		}
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := migrate.ApplySchemaDiffWithOptions(ctx, db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
			t.Fatalf("apply: %v", err)
		}
		var n int
		if err := db.QueryRow(`SELECT n + 1 FROM tallies WHERE id = 'a'`).Scan(&n); err != nil || n != 8 {
			t.Fatalf("n + 1 = %d, %v; want 8", n, err)
		}
	})
}
