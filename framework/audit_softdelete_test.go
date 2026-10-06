package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
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

// The restore/purge audit override is keyed to the entity and record it
// is about: an AfterUpdate hook on entity A that writes a row of entity
// B during A's restore must leave B's audit row saying "update" — the
// operation B's write actually was — while A's own row says "restore".
// Unkeyed, the override stained every nested write the hook chain made.
func TestAudit_RestoreNestedWriteKeepsOwnOp(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Scope: &entity.ScopeConfig{SoftDelete: true},
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		app.Entity("notes", entity.EntityConfig{
			Table: "notes",
			Fields: []schema.Field{
				{Name: "body", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{Actor: func(context.Context) string { return "alice" }})
		posts, err := app.CrudHandler("posts")
		if err != nil {
			t.Fatal(err)
		}
		notes, err := app.CrudHandler("notes")
		if err != nil {
			t.Fatal(err)
		}

		post, err := posts.CreateOne(context.Background(), map[string]any{"title": "hello"})
		if err != nil {
			t.Fatal(err)
		}
		postID, _ := post["id"].(string)
		note, err := notes.CreateOne(context.Background(), map[string]any{"body": "untouched"})
		if err != nil {
			t.Fatal(err)
		}
		noteID, _ := note["id"].(string)
		if err := posts.DeleteOne(context.Background(), postID); err != nil {
			t.Fatal(err)
		}

		// The hook the host registers: on every post update, touch the
		// note. It runs inside RestoreOne with the restore's ctx.
		app.HookRegistry("posts").RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
			_, err := notes.UpdateOne(ctx, noteID, map[string]any{"body": "touched"})
			return err
		})

		if err := posts.RestoreOne(context.Background(), postID); err != nil {
			t.Fatalf("RestoreOne: %v", err)
		}

		ops := map[string]string{}
		for _, r := range readAuditRows(t, db) {
			if r["entity"].(string) == "notes" && r["record_id"].(string) == noteID {
				ops["notes"] = r["op"].(string)
			}
			if r["entity"].(string) == "posts" && r["record_id"].(string) == postID && r["op"].(string) == "restore" {
				ops["posts"] = r["op"].(string)
			}
		}
		if ops["posts"] != "restore" {
			t.Fatalf("the restored post's audit op = %q, want \"restore\"", ops["posts"])
		}
		if ops["notes"] != "update" {
			t.Fatalf("SECURITY: the nested note write's audit op = %q, want \"update\": the restore override leaked into it", ops["notes"])
		}
	})
}

// The same keying leaves one hole the entity check cannot close: an
// AfterUpdate hook that updates the record being restored names the same
// entity and id, so the override matched and the hook's ordinary update
// was audited as a second "restore". Every write entry now starts clean:
// the override answers only for the write that set it.
func TestAudit_RestoreSameRecordUpdateIsUpdate(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, posts := auditSoftDeleteApp(t, db)
		post, err := posts.CreateOne(context.Background(), map[string]any{"title": "hello"})
		if err != nil {
			t.Fatal(err)
		}
		postID, _ := post["id"].(string)
		if err := posts.DeleteOne(context.Background(), postID); err != nil {
			t.Fatal(err)
		}

		touched := false
		app.HookRegistry("posts").RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
			if touched {
				return nil
			}
			touched = true
			_, err := posts.UpdateOne(ctx, postID, map[string]any{"title": "touched"})
			return err
		})

		if err := posts.RestoreOne(context.Background(), postID); err != nil {
			t.Fatalf("RestoreOne: %v", err)
		}

		count := map[string]int{}
		for _, r := range readAuditRows(t, db) {
			if r["record_id"].(string) == postID {
				count[r["op"].(string)]++
			}
		}
		if count["restore"] != 1 || count["update"] != 1 {
			t.Fatalf("audit ops for the post = %v, want one restore and one update", count)
		}
	})
}
