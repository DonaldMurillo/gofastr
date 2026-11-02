package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Boot applies a plain Postgres view with CREATE OR REPLACE VIEW, which
// refuses a body whose columns are not the old ones plus new ones at the
// end. A view that lost, renamed or retyped a column failed every boot
// from then on ("cannot drop columns from view"), while SQLite, which
// drops and creates, booted (2026-11-01 bug bash).
func TestBootReplacesViewWhoseColumnsChanged(t *testing.T) {
	plan := func(sel string) migrate.Plan {
		return migrate.Plan{Views: []migrate.View{
			{Name: "rv1", Select: sel},
			{Name: "rv2", Select: "SELECT id FROM rv1", DependsOn: []string{"rv1"}},
		}}
	}
	ctx := context.Background()
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		fkExec(t, db, "CREATE TABLE rvt (id TEXT PRIMARY KEY, a TEXT, b TEXT)", "INSERT INTO rvt VALUES ('1', 'x', 'yy')")
		if err := migrate.AutoMigratePlanContext(ctx, db, plan("SELECT id, a, b FROM rvt")); err != nil {
			t.Fatal(err)
		}
		for _, sel := range []string{
			"SELECT id, b FROM rvt",                     // a column dropped
			"SELECT id, b AS bee FROM rvt",              // renamed
			"SELECT id, length(b) AS bee FROM rvt",      // retyped
			"SELECT id, length(b) AS bee, a FROM rvt",   // added at the end
			"SELECT id, length(b) AS bee, a FROM rvt  ", // unchanged columns
		} {
			if err := migrate.AutoMigratePlanContext(ctx, db, plan(sel)); err != nil {
				t.Fatalf("boot with rv1 = %q: %v", sel, err)
			}
		}
		var bee int
		if err := db.QueryRow("SELECT bee FROM rv1").Scan(&bee); err != nil || bee != 2 {
			t.Fatalf("rv1.bee = %d (%v), want 2", bee, err)
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM rv2").Scan(&n); err != nil || n != 1 {
			t.Fatalf("the dependent view rv2 = %d rows (%v), want 1", n, err)
		}
	})
}

// A view outside the plan that reads the changed one keeps the refusal:
// boot never drops what it did not declare, and the error is Postgres's.
func TestBootKeepsViewReadOutsideThePlan(t *testing.T) {
	ctx := context.Background()
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("SQLite drops and creates every boot")
		}
		fkExec(t, db, "CREATE TABLE rvt (id TEXT PRIMARY KEY, a TEXT, b TEXT)")
		one := migrate.Plan{Views: []migrate.View{{Name: "rv1", Select: "SELECT id, a FROM rvt"}}}
		if err := migrate.AutoMigratePlanContext(ctx, db, one); err != nil {
			t.Fatal(err)
		}
		fkExec(t, db, "CREATE VIEW hand AS SELECT a FROM rv1")
		changed := migrate.Plan{Views: []migrate.View{{Name: "rv1", Select: "SELECT id, b FROM rvt"}}}
		err := migrate.AutoMigratePlanContext(ctx, db, changed)
		if err == nil || !strings.Contains(err.Error(), "cannot") {
			t.Fatalf("boot = %v, want Postgres's refusal", err)
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM hand").Scan(&n); err != nil {
			t.Fatalf("the hand-made view is gone: %v", err)
		}
	})
}
