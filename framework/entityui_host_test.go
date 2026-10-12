package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// entityUIAuditApp boots one App with audit on and the entity's CRUD
// handlers mounted the way a host mounts them.
func entityUIAuditApp(t *testing.T, db *sql.DB, name string, multiTenant bool) *App {
	t.Helper()
	cfg := entity.EntityConfig{
		Table: name,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
	if multiTenant {
		cfg.Scope = &entity.ScopeConfig{MultiTenant: true}
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity(name, cfg)
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{Actor: func(_ context.Context) string { return "alice" }})
	return app
}

// createNote writes one row through the entity's CRUD handler,
// in-process: the same create path the REST route drives, audit hooks
// included. Returns the new id.
func createNote(t *testing.T, app *App, name string, ctx context.Context, body map[string]any) string {
	t.Helper()
	h, err := app.CrudHandler(name)
	if err != nil {
		t.Fatalf("crud handler: %v", err)
	}
	row, err := h.CreateOne(ctx, body)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return cellOf(row["id"])
}

// updateNote rewrites the row through the same handler's update path.
func updateNote(t *testing.T, app *App, name, id string, ctx context.Context, body map[string]any) {
	t.Helper()
	h, err := app.CrudHandler(name)
	if err != nil {
		t.Fatalf("crud handler: %v", err)
	}
	if _, err := h.UpdateOne(ctx, id, body); err != nil {
		t.Fatalf("update %s %s: %v", name, id, err)
	}
}

func cellOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// TestEntityUIAuditTrail: creating and updating a record through the
// CRUD handler leaves audit rows the entityui Host's Audit().Trail
// reads back — newest first, with the actor, the operation and the
// before/after diff — on SQLite and on Postgres when it is available.
func TestEntityUIAuditTrail(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := entityUIAuditApp(t, db, "notes", false)
		ctx := handler.SetUser(context.Background(), struct{ ID string }{ID: "u1"})
		id := createNote(t, app, "notes", ctx, map[string]any{"title": "first"})
		updateNote(t, app, "notes", id, ctx, map[string]any{"title": "second"})

		reader := entityUIHost{a: app}.Audit()
		if reader == nil {
			t.Fatal("EntityUI's Host exposes no AuditReader although WithAuditLog ran")
		}
		entries, err := reader.Trail(ctx, "notes", id, 50)
		if err != nil {
			t.Fatalf("Trail: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("Trail returned %d entries, want 2 (create, update)", len(entries))
		}
		// Newest first: the update, then the create.
		if entries[0].Operation != "update" || entries[1].Operation != "create" {
			t.Fatalf("operations newest-first, got %q then %q", entries[0].Operation, entries[1].Operation)
		}
		if entries[0].Actor != "alice" {
			t.Fatalf("actor = %q, want alice", entries[0].Actor)
		}
		if entries[0].At.IsZero() {
			t.Fatal("created_at did not scan into the entry's time")
		}
		if v, ok := entries[0].Before["title"].(string); !ok || v != "first" {
			t.Fatalf("update's Before carries the old title, got %#v", entries[0].Before)
		}
		if v, ok := entries[0].After["title"].(string); !ok || v != "second" {
			t.Fatalf("update's After carries the new title, got %#v", entries[0].After)
		}
		if v, ok := entries[1].After["title"].(string); !ok || v != "first" {
			t.Fatalf("create's After carries the row, got %#v", entries[1].After)
		}
	})
}

// TestEntityUIAuditTrailTenantIsolation: a trail read under tenant A
// never returns tenant B's rows, the scope the audit writer stamped.
func TestEntityUIAuditTrailTenantIsolation(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := entityUIAuditApp(t, db, "tnotes", true)
		ids := map[string]string{}
		for _, tn := range []string{"A", "B"} {
			ctx := tenant.SetTenantID(handler.SetUser(context.Background(), struct{ ID string }{ID: "u-" + tn}), tn)
			ids[tn] = createNote(t, app, "tnotes", ctx, map[string]any{"title": "note " + tn})
		}
		reader := entityUIHost{a: app}.Audit()
		if reader == nil {
			t.Fatal("no AuditReader")
		}
		ctxA := tenant.SetTenantID(context.Background(), "A")
		entries, err := reader.Trail(ctxA, "tnotes", ids["B"], 50)
		if err != nil {
			t.Fatalf("Trail: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("SECURITY: tenant A's trail returned tenant B's rows: %+v", entries)
		}
		own, err := reader.Trail(ctxA, "tnotes", ids["A"], 50)
		if err != nil || len(own) != 1 {
			t.Fatalf("tenant A's own trail reads one row: %d, %v", len(own), err)
		}
	})
}
