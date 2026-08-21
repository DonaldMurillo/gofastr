package framework

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// TestMultiVersionMigrate_UnionAddsV2Column proves Rule 1: a column only v2
// declares is created at boot because migrate builds the table schema from
// the union of every version's fields. Without the union, the column would
// never be created and v2's CRUD would hit a runtime SQL error.
func TestMultiVersionMigrate_UnionAddsV2Column(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
		v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
		app.GroupEntity(v1, "posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		app.GroupEntity(v2, "posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "summary", Type: schema.Text}, // v2-only column
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		live, err := migrate.ReadLiveColumns(context.Background(), db, "posts", dialect)
		if err != nil {
			t.Fatalf("read live columns: %v", err)
		}
		if _, ok := live["summary"]; !ok {
			t.Errorf("expected column 'summary' to exist after auto-migrate (union of v1+v2 fields); got columns: %v", live)
		}
		if _, ok := live["title"]; !ok {
			t.Errorf("expected column 'title' to exist; got columns: %v", live)
		}
	})
}

// TestMultiVersionMigrate_ConflictingColumnType proves Rule 2: two versions
// declaring the same column with incompatible physical definitions panic at
// registration, not at migrate time, with a message naming the entity,
// the table, the column, and both versions.
func TestMultiVersionMigrate_ConflictingColumnType(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected GroupEntity to panic on incompatible column types across versions")
		}
		msg := fmt.Sprint(r)
		for _, want := range []string{"posts", "summary", "/api/v1", "/api/v2"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic message missing %q:\n%s", want, msg)
			}
		}
		// The message must explain WHY the conflict is fatal so the author
		// knows to make the definitions agree, not just that it happened.
		if !strings.Contains(msg, "share one table") {
			t.Errorf("panic message should explain the table-sharing reason:\n%s", msg)
		}
	}()

	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "summary", Type: schema.String},
		},
	}.WithTimestamps(false))
	// summary as Int is physically incompatible with v1's String.
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "summary", Type: schema.Int},
		},
	}.WithTimestamps(false))
}

// TestMultiVersionMigrate_ConflictingNullability proves that a nullability
// difference (Required) is a conflict, since NOT NULL is a physical constraint.
func TestMultiVersionMigrate_ConflictingNullability(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on conflicting nullability across versions")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "title") {
			t.Errorf("panic message should name the conflicting column 'title':\n%s", msg)
		}
	}()

	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
}

// TestMultiVersionMigrate_WireOnlyDifferencesRegisterFine proves that
// differences in Hidden, WireName, and field ordering are NOT conflicts.
// They are wire-level concerns that never reach the DDL. Both versions
// register without error and the shared table is migrated once.
func TestMultiVersionMigrate_WireOnlyDifferencesRegisterFine(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("wire-only differences should NOT panic: %v", r)
			}
		}()
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
		v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
		app.GroupEntity(v1, "posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text, Hidden: true}, // v1 hides body
			},
		}.WithTimestamps(false))
		app.GroupEntity(v2, "posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "body", Type: schema.Text, WireName: "content"}, // v2 renames wire key, different order
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		live, err := migrate.ReadLiveColumns(context.Background(), db, "posts", dialect)
		if err != nil {
			t.Fatalf("read live columns: %v", err)
		}
		for _, col := range []string{"id", "title", "body"} {
			if _, ok := live[col]; !ok {
				t.Errorf("expected column %q after union migrate; got: %v", col, live)
			}
		}
	})
}

// TestMultiVersionMigrate_MigratesTableOnce proves the shared table is
// migrated exactly once, not once per version. We register two versions
// and confirm auto-migrate does not error on a duplicate CREATE TABLE or
// duplicate ADD COLUMN (which would happen if each version were migrated
// independently against the same table).
func TestMultiVersionMigrate_MigratesTableOnce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		v1 := app.Group("/api/v1", routegroup.WithMCPNamespace("v1"))
		v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
		app.GroupEntity(v1, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		app.GroupEntity(v2, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))

		// First boot: creates the table.
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("first automigrate: %v", err)
		}
		// Second boot: must be a clean no-op (idempotent), not a duplicate-DDL error.
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("second automigrate should be a no-op: %v", err)
		}
	})
}

// TestMultiVersionConflict_VersionedVsUnversioned proves the conflict check
// also fires when one version is registered via App.Entity (Version == "")
// and another via GroupEntity, both share the table when the name matches.
func TestMultiVersionConflict_VersionedVsUnversioned(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when unversioned and versioned entities share a column with incompatible types")
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, "title") {
			t.Errorf("panic should name column 'title':\n%s", msg)
		}
	}()

	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	v2 := app.Group("/api/v2")
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.Int}}, // incompatible
	}.WithTimestamps(false))
}
