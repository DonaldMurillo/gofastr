package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// runGenerated generates plan against prev, registers the Up on m as the
// given version, runs the migration runner, and returns the next snapshot.
func runGenerated(t *testing.T, m *coremig.Migrator, d Dialect, version uint64, plan migrate.Plan, prev migrate.SchemaSnapshot) migrate.SchemaSnapshot {
	t.Helper()
	up, down, next, err := migrate.GeneratePlan(plan, prev, d)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if err := m.Register(coremig.Migration{Version: version, Name: "gen", Up: up, Down: down}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("runner Up:\n%s\n: %v", up, err)
	}
	return next
}

func viewSource() *Registry {
	reg := NewRegistry()
	_ = reg.Register(entity.Define("src", entity.EntityConfig{
		Table:  "src",
		Fields: []schema.Field{{Name: "n", Type: schema.Int}},
	}.WithTimestamps(false)))
	return reg
}

// New view a selects from new view z. The generator sorted views by name
// after ordering them by dependency, so a was created first and Postgres
// refused it.
func TestGenerateViewsInDependencyOrder(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		plan := migrate.Plan{Registry: viewSource(), Views: []migrate.View{
			{Name: "a", Select: "SELECT id FROM z", DependsOn: []string{"z"}},
			{Name: "z", Select: "SELECT id, n FROM src", DependsOn: []string{"src"}},
		}}
		runGenerated(t, coremig.New(db, coremig.WithDialect(d)), d, 1, plan, emptySnap())
		if _, err := db.Exec("SELECT id FROM a"); err != nil {
			t.Fatalf("view a: %v", err)
		}
	})
}

// Removing a table and the view over it: the generator dropped the table
// first, and Postgres refuses to drop a table a view depends on.
func TestGenerateDropsViewBeforeTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		withView := migrate.Plan{Registry: viewSource(), Views: []migrate.View{
			{Name: "v", Select: "SELECT id, n FROM src", DependsOn: []string{"src"}},
		}}
		m := coremig.New(db, coremig.WithDialect(d))
		snap := runGenerated(t, m, d, 1, withView, emptySnap())
		runGenerated(t, m, d, 2, migrate.Plan{}, snap)
		if _, err := db.Exec("SELECT 1 FROM src"); err == nil {
			t.Fatal("table src survived its removal")
		}
	})
}

// Every boot dropped and recreated a materialized view, and Postgres
// refuses to drop one another view depends on: the second boot failed.
func TestMatViewWithDependentReboots(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	plan := migrate.Plan{Registry: viewSource(), Views: []migrate.View{
		{Name: "mv", Select: "SELECT id, n FROM src", DependsOn: []string{"src"}, Materialized: true},
		{Name: "v", Select: "SELECT id FROM mv", DependsOn: []string{"mv"}},
	}}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if _, err := db.Exec("INSERT INTO src (n) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	// Boot recreates a materialized view, so it refreshes its rows.
	var rows int
	if err := db.QueryRow("SELECT count(*) FROM v").Scan(&rows); err != nil {
		t.Fatalf("view v after the second boot: %v", err)
	}
	if rows != 1 {
		t.Fatalf("v rows after the second boot = %d, want 1 (boot refreshes mv)", rows)
	}
	// A changed definition still recreates the materialized view, taking
	// the plan's dependent view down and back up with it.
	plan.Views[0].Select = "SELECT id, n FROM src WHERE n > 0"
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("boot with a changed definition: %v", err)
	}
	var def string
	if err := db.QueryRow("SELECT definition FROM pg_matviews WHERE matviewname = 'mv'").Scan(&def); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(def, "> 0") {
		t.Fatalf("mv definition = %q, want the changed WHERE", def)
	}
	if _, err := db.Exec("SELECT id FROM v"); err != nil {
		t.Fatalf("view v after the changed boot: %v", err)
	}
}

// Generation passed every routine to the diff, so a SQLite migration
// carried a Postgres-only function that boot would have skipped.
func TestGenerateSkipsOtherDialectRoutine(t *testing.T) {
	plan := migrate.Plan{Routines: []migrate.Routine{{
		Name:    "pg_only",
		Up:      "CREATE OR REPLACE FUNCTION pg_only() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql",
		Down:    "DROP FUNCTION IF EXISTS pg_only()",
		Dialect: DialectPostgres,
	}}}
	up, down, next, err := migrate.GeneratePlan(plan, emptySnap(), DialectSQLite)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if strings.Contains(up+down, "FUNCTION") {
		t.Fatalf("a SQLite migration carries the Postgres-only routine:\nup: %s\ndown: %s", up, down)
	}
	// Recorded, it would read as created, and dropping it from the plan
	// would generate a DROP FUNCTION for SQLite.
	if _, ok := next.Routines["pg_only"]; ok {
		t.Fatal("the SQLite snapshot records the Postgres-only routine")
	}
}
