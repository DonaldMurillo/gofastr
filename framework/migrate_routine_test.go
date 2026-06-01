package framework

import (
	"context"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func emptySnap() migrate.SchemaSnapshot {
	return migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}
}

// TestRoutine_GenerateNew: a brand-new routine emits its Up forward and its
// Down for rollback, and lands in the snapshot.
func TestRoutine_GenerateNew(t *testing.T) {
	plan := migrate.Plan{Routines: []migrate.Routine{{
		Name: "f", Up: "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql",
		Down: "DROP FUNCTION IF EXISTS f()",
	}}}
	up, down, next, err := migrate.GeneratePlan(plan, emptySnap(), DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !strings.Contains(up, "CREATE OR REPLACE FUNCTION f") {
		t.Fatalf("up missing routine create: %s", up)
	}
	if !strings.Contains(down, "DROP FUNCTION IF EXISTS f") {
		t.Fatalf("down missing routine drop: %s", down)
	}
	if next.Routines["f"].Up == "" {
		t.Fatal("routine not recorded in next snapshot")
	}
}

// TestRoutine_GenerateChangedRestoresPrevious: a changed routine emits the new
// body forward and the PREVIOUS body as the Down (true reversibility).
func TestRoutine_GenerateChangedRestoresPrevious(t *testing.T) {
	prev := emptySnap()
	prev.Routines = map[string]migrate.RoutineDef{
		"f": {Up: "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql", Down: "DROP FUNCTION IF EXISTS f()"},
	}
	plan := migrate.Plan{Routines: []migrate.Routine{{
		Name: "f", Up: "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 2 $$ LANGUAGE sql",
		Down: "DROP FUNCTION IF EXISTS f()",
	}}}
	up, down, _, err := migrate.GeneratePlan(plan, prev, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !strings.Contains(up, "SELECT 2") {
		t.Fatalf("up should carry the new body: %s", up)
	}
	if !strings.Contains(down, "SELECT 1") {
		t.Fatalf("down should restore the previous body: %s", down)
	}
}

// TestRoutine_GenerateRemoved: removing a routine drops it forward and recreates
// it on rollback.
func TestRoutine_GenerateRemoved(t *testing.T) {
	prev := emptySnap()
	prev.Routines = map[string]migrate.RoutineDef{
		"f": {Up: "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql", Down: "DROP FUNCTION IF EXISTS f()"},
	}
	up, down, _, err := migrate.GeneratePlan(migrate.Plan{}, prev, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if !strings.Contains(up, "DROP FUNCTION IF EXISTS f") {
		t.Fatalf("up should drop the removed routine: %s", up)
	}
	if !strings.Contains(down, "CREATE OR REPLACE FUNCTION f") {
		t.Fatalf("down should recreate the removed routine: %s", down)
	}
}

// TestRoutine_GenerateUnchanged: regenerating with no change produces nothing.
func TestRoutine_GenerateUnchanged(t *testing.T) {
	plan := migrate.Plan{Routines: []migrate.Routine{{Name: "f", Up: "CREATE VIEW v AS SELECT 1", Down: "DROP VIEW v"}}}
	_, _, snap, _ := migrate.GeneratePlan(plan, emptySnap(), DialectPostgres)
	up, _, _, err := migrate.GeneratePlan(plan, snap, DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	if up != "" {
		t.Fatalf("expected no changes for an unchanged routine, got: %s", up)
	}
}

// TestRoutine_OrderingTableThenRoutine: when a migration creates a table AND a
// routine, the Down must drop the routine BEFORE the table it depends on.
func TestRoutine_OrderingTableThenRoutine(t *testing.T) {
	reg := NewRegistry()
	reg.Register(entity.Define("widgets", entity.EntityConfig{
		Table: "widgets", Fields: []schema.Field{{Name: "n", Type: schema.Int}},
	}.WithTimestamps(false)))
	plan := migrate.Plan{Registry: reg, Routines: []migrate.Routine{{
		Name: "widgets_view", Up: "CREATE VIEW widgets_view AS SELECT * FROM widgets", Down: "DROP VIEW widgets_view",
	}}}
	up, down, _, err := migrate.GeneratePlan(plan, emptySnap(), DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}
	// Up: table before view.
	if strings.Index(up, "CREATE TABLE") > strings.Index(up, "widgets_view") {
		t.Fatalf("up should create the table before the view:\n%s", up)
	}
	// Down: drop view before table.
	if strings.Index(down, "DROP VIEW widgets_view") > strings.Index(down, "DROP TABLE") {
		t.Fatalf("down should drop the view before the table:\n%s", down)
	}
}

// TestRoutine_AutoMigratePostgres exercises a real stored function end-to-end:
// AutoMigratePlanContext creates it (idempotently) and it's callable.
func TestRoutine_AutoMigratePostgres(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()
	plan := migrate.Plan{
		Routines: []migrate.Routine{{
			Name: "double_it",
			Up:   "CREATE OR REPLACE FUNCTION double_it(x integer) RETURNS integer AS $$ BEGIN RETURN x * 2; END; $$ LANGUAGE plpgsql",
			Down: "DROP FUNCTION IF EXISTS double_it(integer)",
		}},
	}
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext: %v", err)
	}
	// Idempotent re-run (CREATE OR REPLACE).
	if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
		t.Fatalf("AutoMigratePlanContext (re-run): %v", err)
	}
	var got int
	if err := db.QueryRow("SELECT double_it(21)").Scan(&got); err != nil {
		t.Fatalf("call function: %v", err)
	}
	if got != 42 {
		t.Fatalf("double_it(21) = %d, want 42", got)
	}
}

// TestRoutine_GeneratedAppliesAndRollsBack proves a generated routine migration
// round-trips through the runner on real Postgres: apply (function exists),
// roll back (function gone).
func TestRoutine_GeneratedAppliesAndRollsBack(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	ctx := context.Background()

	plan := migrate.Plan{Routines: []migrate.Routine{{
		Name: "greet",
		Up:   "CREATE OR REPLACE FUNCTION greet() RETURNS text AS $$ SELECT 'hi'::text $$ LANGUAGE sql",
		Down: "DROP FUNCTION IF EXISTS greet()",
	}}}
	up, down, _, err := migrate.GeneratePlan(plan, emptySnap(), DialectPostgres)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	m := coremig.New(db, coremig.WithDialect(coremig.DialectPostgres))
	m.Register(coremig.Migration{Version: 1, Name: "add_greet", Up: up, Down: down})
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	var s string
	if err := db.QueryRow("SELECT greet()").Scan(&s); err != nil {
		t.Fatalf("call greet after up: %v", err)
	}
	if s != "hi" {
		t.Fatalf("greet() = %q, want hi", s)
	}
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := db.QueryRow("SELECT greet()").Scan(&s); err == nil {
		t.Fatal("greet() should not exist after rollback")
	}
}
