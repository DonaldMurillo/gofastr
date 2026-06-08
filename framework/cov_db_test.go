package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// ============================================================================
// tx.go — InTx no-DB error branch
// ============================================================================

func TestCovInTxNoDB(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	err := app.InTx(context.Background(), func(context.Context, *sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("expected no-DB error from InTx")
	}
}

// InTx surfaces the BeginTx error (tx.go line 32) when the DB is closed.
func TestCovInTxBeginError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		_ = db.Close()
		err := app.InTx(context.Background(), func(context.Context, *sql.Tx) error { return nil })
		if err == nil {
			t.Fatal("expected BeginTx error on closed DB")
		}
	})
}

// ============================================================================
// app.go — CrudHandler / MustCrudHandler error branches
// ============================================================================

func TestCovCrudHandlerErrors(t *testing.T) {
	// No DB → error.
	app := NewApp(WithoutDefaultMiddleware())
	if _, err := app.CrudHandler("posts"); err == nil {
		t.Fatal("expected no-DB error")
	}

	// DB present but entity unregistered → error.
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		if _, err := app.CrudHandler("missing"); err == nil {
			t.Fatal("expected unknown-entity error")
		}
		// MustCrudHandler panics on the same.
		defer func() {
			if recover() == nil {
				t.Fatal("expected MustCrudHandler panic")
			}
		}()
		app.MustCrudHandler("missing")
	})
}

// ============================================================================
// app.go — Entity MCP-with-CRUD-false guard (requires DB)
// ============================================================================

func TestCovEntityMCPWithoutCRUDPanics(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic: MCP=true with CRUD=false")
			}
		}()
		app.Entity("posts", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
			CRUD:   boolPtr(false),
		})
	})
}

// GroupEntity with the same misconfiguration panics (group path).
func TestCovGroupEntityMCPWithoutCRUDPanics(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		g := app.Group("/api")
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic: group MCP=true with CRUD=false")
			}
		}()
		app.GroupEntity(g, "posts", entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
			CRUD:   boolPtr(false),
		})
	})
}

// Entity panics when RegisterEntityMCPTools fails (app.go line 597): a
// pre-registered tool name collides with a generated CRUD tool name.
func TestCovEntityMCPRegisterError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		// Pre-claim "posts_list" so the entity's MCP registration collides.
		if err := app.MCP.RegisterTool("posts_list", "x", map[string]any{"type": "object"},
			func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic from colliding MCP CRUD tool name")
			}
		}()
		app.Entity("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
		}.WithTimestamps(false))
	})
}

// GroupEntity panics when its MCP CRUD-tool registration fails (app.go
// line 401).
func TestCovGroupEntityMCPRegisterError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		if err := app.MCP.RegisterTool("posts_list", "x", map[string]any{"type": "object"},
			func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
		g := app.Group("/api")
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic from colliding group MCP CRUD tool name")
			}
		}()
		app.GroupEntity(g, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
		}.WithTimestamps(false))
	})
}

// GroupEntity happy path WITH DB: CRUD routes + MCP tools register.
func TestCovGroupEntityWithDB(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		g := app.Group("/api", routegroup.WithMCPNamespace("api"))
		app.GroupEntity(g, "posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			MCP:    true,
		}.WithTimestamps(false))
		if _, err := app.Registry.Get("posts"); err != nil {
			t.Fatalf("entity not registered: %v", err)
		}
		// MCP CRUD tools should be present.
		if len(app.MCP.ListTools()) == 0 {
			t.Fatal("expected MCP CRUD tools for group entity")
		}
	})
}

// ============================================================================
// app.go — Table / View / Routine registration
// ============================================================================

func TestCovTableAndView(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())

		// Raw migration-only table.
		app.Table(migrate.Table{
			Name: "raw_events",
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true},
				{Name: "kind", Type: schema.String},
			},
		})
		if _, err := app.Registry.Get("raw_events"); err != nil {
			t.Fatalf("table not registered: %v", err)
		}

		// View WITH columns → registered as a read-only ORM entity.
		app.View(migrate.View{
			Name:      "events_view",
			Select:    "SELECT id, kind FROM raw_events",
			DependsOn: []string{"raw_events"},
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true},
				{Name: "kind", Type: schema.String},
			},
		})
		if _, err := app.Registry.Get("events_view"); err != nil {
			t.Fatalf("view entity not registered: %v", err)
		}

		// View WITHOUT columns → migration-only, no entity (early return).
		app.View(migrate.View{Name: "noop_view", Select: "SELECT 1"})
		if _, err := app.Registry.Get("noop_view"); err == nil {
			t.Fatal("columnless view should not register an ORM entity")
		}

		// Routine append.
		app.Routine(migrate.Routine{Name: "noop_fn", Up: "SELECT 1"})
	})
}

// Table duplicate registration panics.
func TestCovTableDuplicatePanics(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Table(migrate.Table{
		Name:    "dup",
		Columns: []migrate.Column{{Name: "id", Type: schema.String, PrimaryKey: true}},
	})
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate-table panic")
		}
	}()
	app.Table(migrate.Table{
		Name:    "dup",
		Columns: []migrate.Column{{Name: "id", Type: schema.String, PrimaryKey: true}},
	})
}

// ============================================================================
// health.go — dbReadinessCheck actually pings the DB
// ============================================================================

func TestCovDBReadinessCheck(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		c := dbReadinessCheck(app)
		if c.Name != "db" {
			t.Fatalf("check name = %q", c.Name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Check(ctx); err != nil {
			t.Fatalf("db readiness ping failed: %v", err)
		}
	})
}
