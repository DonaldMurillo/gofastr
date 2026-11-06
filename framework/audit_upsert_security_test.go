package framework

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestUpsertAuditsItsWriteArm(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.WithAuditLog(AuditConfig{})
	created, updated := 0, 0
	app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(context.Context, any) error { created++; return nil })
	app.HookRegistry("posts").RegisterHook(hook.AfterUpdate, func(context.Context, any) error { updated++; return nil })
	ch, err := app.CrudHandler("posts")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []map[string]any{
		{"id": "p1", "title": "before"},
		{"id": "p1", "title": "after"},
		{"id": "p1"},
	} {
		if _, err := ch.UpsertOne(context.Background(), body); err != nil {
			t.Fatal(err)
		}
	}
	if created != 1 || updated != 1 {
		t.Errorf("after hooks: create=%d update=%d, want 1 each", created, updated)
	}
	rows := readAuditRows(t, db)
	if len(rows) != 2 {
		t.Errorf("audit rows=%d, want insert and update only", len(rows))
	}
	if len(rows) < 2 {
		t.Fatal("missing audit rows")
	}
	if rows[0]["op"] != "create" || rows[1]["op"] != "update" {
		t.Errorf("audit operations=%v, %v, want create, update", rows[0]["op"], rows[1]["op"])
	}
	var diff map[string]map[string]any
	if err := json.Unmarshal([]byte(rows[1]["diff"].(string)), &diff); err != nil {
		t.Fatal(err)
	}
	if diff["old"]["title"] != "before" || diff["new"]["title"] != "after" {
		t.Errorf("update images=%v, want before and after", diff)
	}
}
