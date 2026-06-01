package framework

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func usersWithActive() *Registry {
	reg := NewRegistry()
	reg.Register(entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "active", Type: schema.Bool},
		},
	}.WithTimestamps(false)))
	return reg
}

func activeUsersView() migrate.View {
	return migrate.View{
		Name:      "active_users",
		Select:    "SELECT id, name FROM users WHERE active",
		DependsOn: []string{"users"},
		Columns: []migrate.Column{
			{Name: "id", Type: schema.String, PrimaryKey: true},
			{Name: "name", Type: schema.String},
		},
	}
}

// TestView_MigratesIdempotentAndQueryable: a view built from an entity is
// created after its table, re-applies idempotently, and is queryable.
func TestView_MigratesIdempotentAndQueryable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		plan := migrate.Plan{Registry: usersWithActive(), Views: []migrate.View{activeUsersView()}}
		ctx := context.Background()
		if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		if err := migrate.AutoMigratePlanContext(ctx, db, plan); err != nil {
			t.Fatalf("re-run (idempotent): %v", err)
		}
		db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u1", "alice", true)
		db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u2", "bob", false)
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM active_users").Scan(&n); err != nil {
			t.Fatalf("query view: %v", err)
		}
		if n != 1 {
			t.Fatalf("expected 1 active user, got %d", n)
		}
	})
}

// TestView_UnmanagedSkipsTableDDL: registering a view's entity (Unmanaged) must
// NOT create a table for it.
func TestView_UnmanagedSkipsTableDDL(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(activeUsersView().ToEntity()) // Unmanaged entity
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cols, _ := migrate.ReadLiveColumns(context.Background(), db, "active_users", migrate.DetectDialect(db))
		if len(cols) != 0 {
			t.Fatalf("AutoMigrate created a table for an Unmanaged view entity: %v", keysOf(cols))
		}
		// And DiffSchema reports nothing for it.
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("Unmanaged entity produced diff changes: %+v", changes)
		}
	})
}

// TestView_GenerateReversible: a generated view migration creates the view
// forward and drops it on rollback, round-tripping through the runner.
func TestView_GenerateReversible(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		plan := migrate.Plan{Registry: usersWithActive(), Views: []migrate.View{activeUsersView()}}
		up, down, _, err := migrate.GeneratePlan(plan, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, dialect)
		if err != nil {
			t.Fatalf("GeneratePlan: %v", err)
		}
		if !strings.Contains(up, "active_users") || !strings.Contains(strings.ToUpper(up), "VIEW") {
			t.Fatalf("up missing view DDL:\n%s", up)
		}
		if !strings.Contains(strings.ToUpper(down), "DROP") || !strings.Contains(down, "active_users") {
			t.Fatalf("down missing view drop:\n%s", down)
		}

		m := coremig.New(db, coremig.WithDialect(dialect))
		m.Register(coremig.Migration{Version: 1, Name: "schema", Up: up, Down: down})
		ctx := context.Background()
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		if _, err := db.Exec("SELECT * FROM active_users"); err != nil {
			t.Fatalf("view should exist after up: %v", err)
		}
		if err := m.Down(ctx, 1); err != nil {
			t.Fatalf("Down: %v", err)
		}
		if _, err := db.Exec("SELECT * FROM active_users"); err == nil {
			t.Fatal("view should be gone after rollback")
		}
	})
}

// TestView_ORMReadOnly: App.View exposes the view as a read-only ORM entity —
// List works and returns the view's rows, while write routes are not mounted.
func TestView_ORMReadOnly(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("users", entity.EntityConfig{
			Table:  "users",
			Fields: []schema.Field{{Name: "name", Type: schema.String}, {Name: "active", Type: schema.Bool}},
		}.WithTimestamps(false)))
		app.View(activeUsersView())

		// Boot-equivalent migrate: tables + the view.
		plan := migrate.Plan{Registry: app.Registry, Views: app.migrationViews}
		if err := migrate.AutoMigratePlanContext(context.Background(), db, plan); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u1", "alice", true)
		db.Exec("INSERT INTO users (id, name, active) VALUES ($1,$2,$3)", "u2", "bob", false)

		// The view is registered as an Unmanaged ORM entity; read it.
		ve, err := app.Registry.Get("active_users")
		if err != nil {
			t.Fatalf("view entity not registered: %v", err)
		}
		if !ve.Config.Unmanaged {
			t.Error("view entity should be Unmanaged")
		}
		ch := crud.NewCrudHandler(ve, db)
		rows, err := ch.ListAll(context.Background(), crud.ListOptions{})
		if err != nil {
			t.Fatalf("ListAll on view: %v", err)
		}
		if len(rows) != 1 || rows[0]["name"] != "alice" {
			t.Fatalf("expected the view to return only active alice, got %+v", rows)
		}

		// Read routes mounted, write routes NOT.
		ta := TestHarness(t, app)
		defer ta.Close()
		ta.Get("/active_users").AssertStatus(t, http.StatusOK)
		resp := ta.Post("/active_users", map[string]any{"name": "x"})
		if resp.Status() == http.StatusOK || resp.Status() == http.StatusCreated {
			t.Fatalf("write to a read-only view should be rejected, got %d", resp.Status())
		}
	})
}
