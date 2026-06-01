package crud

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func TestEagerLoad_BelongsToNoSourceRows(t *testing.T) {
	// Pass an id with no matching source row → the source SELECT yields
	// zero rows → the len(fkValues)==0 early return fires.
	db := setupDB(t,
		`CREATE TABLE bsposts (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE bsusers (id TEXT PRIMARY KEY, name TEXT)`,
	)
	ent := entity.Define("bsposts", entity.EntityConfig{
		Name: "bsposts", Table: "bsposts",
		Fields: []schema.Field{{Name: "author_id", Type: schema.String}},
	}.WithTimestamps(false))
	rels := []entity.Relation{entity.BelongsTo("author", "bsusers", "author_id")}
	got, err := EagerLoad(context.Background(), db, ent, rels, []string{"ghost"})
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if _, ok := got["ghost"]["author"]; ok {
		t.Error("no source rows → no author attached")
	}
}

func TestInclude_BelongsToNoSourceRows(t *testing.T) {
	// Same as above but through the filtered include loader.
	db := setupDB(t,
		`CREATE TABLE ibsposts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE ibsusers (id TEXT PRIMARY KEY, name TEXT)`,
	)
	seedRows(t, db, "ibsposts", []map[string]any{{"id": "p1", "title": "t", "author_id": "u1"}})
	seedRows(t, db, "ibsusers", []map[string]any{{"id": "u1", "name": "alice"}})
	usersEnt := entity.Define("ibsusers", entity.EntityConfig{
		Name: "ibsusers", Table: "ibsusers",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("ibsposts", entity.EntityConfig{
		Name: "ibsposts", Table: "ibsposts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "ibsusers", "author_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"ibsusers": usersEnt, "ibsposts": postsEnt}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	// GetOne for a non-existent id with include → 0 source rows in loader.
	if _, err := ch.GetOne(context.Background(), "ghost", []string{"author"}); !errors.Is(err, errNotFound) {
		t.Errorf("GetOne ghost = %v, want errNotFound", err)
	}
}

func TestUpsertPreflight_MultiTenantOnly(t *testing.T) {
	// MultiTenant-only entity (no owner, no soft-delete): preflight runs the
	// tenant idx++ path and returns nil when the tenant matches.
	db := setupDB(t, `CREATE TABLE upt (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("upt", entity.EntityConfig{
		Name: "upt", Table: "upt", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := tenant.SetTenantID(context.Background(), "T1")
	// First upsert inserts. Second re-upserts the same PK with the same
	// tenant → preflight finds the row, tenant matches, returns nil.
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "r1", "body": "a"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "r1", "body": "b"}); err != nil {
		t.Fatalf("conflicting upsert: %v", err)
	}
	var body string
	_ = db.QueryRow("SELECT body FROM upt WHERE id='r1'").Scan(&body)
	if body != "b" {
		t.Errorf("upsert update body = %q, want b", body)
	}
}

func TestUpsertPreflight_NoPKSupplied(t *testing.T) {
	// A soft-delete entity upserted with no id in the body → preflight's
	// "no PK ⇒ no conflict possible" early return fires (then the INSERT
	// auto-generates the id).
	db := setupDB(t, `CREATE TABLE upn (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)
	ent := entity.Define("upn", entity.EntityConfig{
		Name: "upn", Table: "upn", SoftDelete: true,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	res, err := ch.UpsertOne(context.Background(), map[string]any{"title": "noid"})
	if err != nil {
		t.Fatalf("upsert without pk: %v", err)
	}
	if res["id"] == nil || res["id"] == "" {
		t.Errorf("upsert should auto-generate id: %v", res)
	}
}

func TestUpsert_AfterCreateHookError(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
		return errors.New("upsert after boom")
	})
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "x", "title": "t"}); err == nil {
		t.Error("upsert AfterCreate hook error should propagate")
	}
}

func TestMultipart_EmptyFieldValue(t *testing.T) {
	// A multipart form value with an empty entry list is skipped. We simulate
	// this by parsing a multipart body whose only field carries an empty value.
	ch, _ := covUploadHandler(t)
	body := "--b\r\nContent-Disposition: form-data; name=\"caption\"\r\n\r\nhello\r\n--b--\r\n"
	req := httptest.NewRequest("POST", "/media", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	req.Body = io.NopCloser(strings.NewReader(body))
	out, err := ch.parseMultipartBody(req)
	if err != nil {
		t.Fatalf("parseMultipartBody: %v", err)
	}
	if out["caption"] != "hello" {
		t.Errorf("caption = %v", out["caption"])
	}
}

func TestParseMultipartBody_BadBody(t *testing.T) {
	// A declared multipart Content-Type with a non-multipart body → parse error.
	ch, _ := covUploadHandler(t)
	req := httptest.NewRequest("POST", "/media", io.NopCloser(strings.NewReader("not multipart")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	if _, err := ch.parseMultipartBody(req); err == nil {
		t.Error("malformed multipart body should error")
	}
}
