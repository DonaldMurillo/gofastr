package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// auditSoftDeleteApp builds a soft-delete posts app with the audit helper
// and returns its CrudHandler so the in-process RestoreOne/PurgeOne can
// run against the same hook chain an HTTP write runs through.
func auditSoftDeleteApp(t *testing.T, db *sql.DB) (*App, *crud.CrudHandler) {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Scope: &entity.ScopeConfig{SoftDelete: true},
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{Actor: func(context.Context) string { return "alice" }})
	handler, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil {
		t.Fatal("no crud handler for posts")
	}
	return app, handler
}

// RestoreOne and PurgeOne are an update and a delete in mechanism, but the
// audit trail must say what actually happened: the op column reads
// "restore" and "purge", not "update" and "delete". This pins the
// crud.withAuditOperation seam WithAuditLog consults.
func TestAudit_RestoreAndPurgeOps(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, handler := auditSoftDeleteApp(t, db)
		created, err := handler.CreateOne(context.Background(), map[string]any{"title": "hello"})
		if err != nil {
			t.Fatal(err)
		}
		id, _ := created["id"].(string)
		if err := handler.DeleteOne(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if err := handler.RestoreOne(context.Background(), id); err != nil {
			t.Fatalf("RestoreOne: %v", err)
		}
		if err := handler.DeleteOne(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if err := handler.PurgeOne(context.Background(), id); err != nil {
			t.Fatalf("PurgeOne: %v", err)
		}

		rows := readAuditRows(t, db)
		var ops []string
		for _, r := range rows {
			ops = append(ops, r["op"].(string))
		}
		want := []string{"create", "delete", "restore", "delete", "purge"}
		if len(ops) != len(want) {
			t.Fatalf("audit ops = %v, want %v", ops, want)
		}
		for i := range want {
			if ops[i] != want[i] {
				t.Fatalf("audit ops = %v, want %v", ops, want)
			}
		}
	})
}
