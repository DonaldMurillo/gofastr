package framework

// Real-Postgres coverage for the migration features that were under-tested:
// materialized views, multi-dependency views, and the non-function routine
// kinds (triggers, procedures). Plain views, raw tables, and function routines
// are covered elsewhere (migrate_view_test.go, migrate_table_test.go,
// migrate_routine_test.go). These run PG-only because they exercise
// Postgres-specific DDL (MATERIALIZED VIEW, plpgsql triggers, CREATE PROCEDURE).

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// #29 — a materialized view is created as MATERIALIZED (not a plain view),
// re-applies idempotently (DROP+CREATE), holds a refreshable snapshot, and
// generates a reversible DROP.
func TestMatView_MigratesQueryableReversible(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	view := migrate.View{
		Name:         "active_users_mv",
		Select:       "SELECT id, name FROM users WHERE active",
		DependsOn:    []string{"users"},
		Materialized: true,
		Columns: []migrate.Column{
			{Name: "id", Type: schema.String, PrimaryKey: true},
			{Name: "name", Type: schema.String},
		},
	}
	plan := migrate.Plan{Registry: usersWithActive(), Views: []migrate.View{view}}

	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("migrate materialized view: %v", err)
	}
	// It is a MATERIALIZED view, not a plain one.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM pg_matviews WHERE matviewname='active_users_mv'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("expected active_users_mv in pg_matviews (materialized), got none")
	}
	// Idempotent re-apply (matviews can't OR REPLACE; the migration DROP+CREATEs).
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("re-apply materialized view: %v", err)
	}
	// Snapshot semantics: insert, REFRESH, query.
	if _, err := db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u1", "alice", true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u2", "bob", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("REFRESH MATERIALIZED VIEW active_users_mv"); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var cnt int
	if err := db.QueryRow("SELECT COUNT(*) FROM active_users_mv").Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Fatalf("materialized view rows=%d, want 1 (only active)", cnt)
	}
	// Generate emits MATERIALIZED VIEW forward and a reversible DROP.
	up, down, _, err := migrate.GeneratePlan(plan, emptySnap(), DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !strings.Contains(up, "MATERIALIZED VIEW") {
		t.Fatalf("generated up should CREATE MATERIALIZED VIEW: %s", up)
	}
	if !strings.Contains(down, "DROP MATERIALIZED VIEW") {
		t.Fatalf("generated down should DROP MATERIALIZED VIEW: %s", down)
	}
}

// #30 — a view that DependsOn two tables is created after BOTH (topo order)
// and is queryable.
func TestView_MultiDependencyTopoOrder(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	reg := NewRegistry()
	if err := reg.Register(entity.Define("users", entity.EntityConfig{Table: "users", Fields: []schema.Field{{Name: "name", Type: schema.String}}}.WithTimestamps(false))); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(entity.Define("orders", entity.EntityConfig{Table: "orders", Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "total", Type: schema.Int}}}.WithTimestamps(false))); err != nil {
		t.Fatal(err)
	}
	view := migrate.View{
		Name:      "user_orders",
		DependsOn: []string{"users", "orders"},
		Select:    "SELECT u.id AS uid, u.name, o.total FROM users u JOIN orders o ON o.user_id = u.id",
	}
	plan := migrate.Plan{Registry: reg, Views: []migrate.View{view}}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("migrate view depending on two tables: %v", err)
	}
	if _, err := db.Exec("SELECT * FROM user_orders"); err != nil {
		t.Fatalf("multi-dependency view not queryable: %v", err)
	}
}

// #30 — a trigger routine (table + plpgsql function + trigger) actually fires.
func TestRoutine_TriggerFiresOnPostgres(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	reg := NewRegistry()
	if err := reg.Register(entity.Define("events", entity.EntityConfig{Table: "events", Fields: []schema.Field{{Name: "stamped", Type: schema.Bool}}}.WithTimestamps(false))); err != nil {
		t.Fatal(err)
	}
	plan := migrate.Plan{
		Registry: reg,
		Routines: []migrate.Routine{
			{
				Name: "stamp_fn",
				Up:   "CREATE OR REPLACE FUNCTION stamp() RETURNS trigger AS $$ BEGIN NEW.stamped := true; RETURN NEW; END; $$ LANGUAGE plpgsql",
				Down: "DROP FUNCTION IF EXISTS stamp() CASCADE",
			},
			{
				Name: "stamp_trg",
				Up:   "CREATE OR REPLACE TRIGGER stamp_trg BEFORE INSERT ON events FOR EACH ROW EXECUTE FUNCTION stamp()",
				Down: "DROP TRIGGER IF EXISTS stamp_trg ON events",
			},
		},
	}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("migrate trigger: %v", err)
	}
	if _, err := db.Exec("INSERT INTO events (id, stamped) VALUES ($1, false)", "e1"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var stamped bool
	if err := db.QueryRow("SELECT stamped FROM events WHERE id=$1", "e1").Scan(&stamped); err != nil {
		t.Fatal(err)
	}
	if !stamped {
		t.Fatal("BEFORE INSERT trigger did not fire (stamped should be true)")
	}
}

// #30 — a stored procedure routine is created and CALL-able on Postgres.
func TestRoutine_ProcedureOnPostgres(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	reg := NewRegistry()
	if err := reg.Register(entity.Define("counters", entity.EntityConfig{Table: "counters", Fields: []schema.Field{{Name: "n", Type: schema.Int}}}.WithTimestamps(false))); err != nil {
		t.Fatal(err)
	}
	plan := migrate.Plan{
		Registry: reg,
		Routines: []migrate.Routine{{
			Name: "bump",
			Up:   "CREATE OR REPLACE PROCEDURE bump(rid text) LANGUAGE plpgsql AS $$ BEGIN UPDATE counters SET n = n + 1 WHERE id = rid; END; $$",
			Down: "DROP PROCEDURE IF EXISTS bump(text)",
		}},
	}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("migrate procedure: %v", err)
	}
	if _, err := db.Exec("INSERT INTO counters (id, n) VALUES ($1, $2)", "c1", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CALL bump($1)", "c1"); err != nil {
		t.Fatalf("CALL procedure: %v", err)
	}
	var got int
	if err := db.QueryRow("SELECT n FROM counters WHERE id=$1", "c1").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("procedure did not run: n=%d, want 1", got)
	}
	_ = sql.ErrNoRows
}
