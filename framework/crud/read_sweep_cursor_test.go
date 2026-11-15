package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

func sweepCursorPage(t *testing.T, ch *CrudHandler, query string) (int, pagination.CursorPage, string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?"+query, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var page pagination.CursorPage
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode page: %v", err)
		}
	}
	return rec.Code, page, rec.Body.String()
}

func seqCursor(c *entity.EntityConfig) {
	c.Pagination = &entity.PaginationConfig{CursorField: "seq"}
}

// A key-only cursor reads its anchor row under the page's BeforeList
// scope: a row the hook hides marks no position.
func TestCursorAnchorKeepsBeforeListScope(t *testing.T) {
	ch, _ := covItems(t, seqCursor, 5)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("seq >= $1", 2)
		return nil
	})
	cur := url.QueryEscape(pagination.EncodeCursor("id", "a"))
	code, _, body := sweepCursorPage(t, ch, "limit=2&cursor="+cur)
	if code != http.StatusBadRequest || !strings.Contains(body, "cursor_anchor_gone") {
		t.Fatalf("cursor on a hook-hidden row = %d %s, want 400 cursor_anchor_gone", code, body)
	}
}

// A soft-deleted row is gone to a key-only cursor, as to every read.
func TestCursorAnchorSkipsSoftDeleted(t *testing.T) {
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, seq INTEGER NOT NULL, name TEXT, deleted_at TIMESTAMP)`)
	ent := entity.Define("items", entity.EntityConfig{
		Name: "items", Table: "items",
		Fields: []schema.Field{{Name: "seq", Type: schema.Int, Required: true}, {Name: "name", Type: schema.String}},
		Scope:  &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	seqCursor(&ent.Config)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for i, id := range []string{"a", "b", "c"} {
		if _, err := db.Exec("INSERT INTO items (id, seq, name) VALUES (?, ?, 'n')", id, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("UPDATE items SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'a'"); err != nil {
		t.Fatal(err)
	}
	cur := url.QueryEscape(pagination.EncodeCursor("id", "a"))
	code, _, body := sweepCursorPage(t, ch, "limit=1&cursor="+cur)
	if code != http.StatusBadRequest || !strings.Contains(body, "cursor_anchor_gone") {
		t.Fatalf("cursor on a soft-deleted row = %d %s, want 400 cursor_anchor_gone", code, body)
	}
}

// A cursor column the projection leaves out (an auto timestamp, which
// need not be Required) is read for the next cursor and kept off the rows.
func TestCursorUnprojectedColumnStaysOffRows(t *testing.T) {
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT, created_at TIMESTAMP, updated_at TIMESTAMP)`)
	cfg := entity.EntityConfig{
		Name: "items", Table: "items",
		Fields:     []schema.Field{{Name: "name", Type: schema.String}},
		Pagination: &entity.PaginationConfig{CursorField: "created_at"},
	}.WithTimestamps(true)
	ent := entity.Define("items", cfg)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := ch.CreateOne(context.Background(), map[string]any{"id": id, "name": "n"}); err != nil {
			t.Fatal(err)
		}
	}
	code, page, body := sweepCursorPage(t, ch, "limit=2&fields=name&cursor=")
	if code != http.StatusOK || !page.HasMore || page.Cursor == "" {
		t.Fatalf("first page = %d %s", code, body)
	}
	for _, row := range page.Data {
		if _, ok := row["created_at"]; ok {
			t.Fatalf("?fields=name row carried the cursor column: %v", row)
		}
	}
	first := page.Data
	code, page, body = sweepCursorPage(t, ch, "limit=2&fields=name&cursor="+url.QueryEscape(page.Cursor))
	if code != http.StatusOK || len(page.Data) != 1 || page.Data[0]["id"] == first[0]["id"] || page.Data[0]["id"] == first[1]["id"] {
		t.Fatalf("second page = %d %s, want the third row", code, body)
	}
}

// AfterList sees the same payload on a keyset page as on an offset page,
// the clauses BeforeList added included.
func TestCursorAfterListSeesWhere(t *testing.T) {
	ch, _ := covItems(t, seqCursor, 3)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("seq >= $1", 0)
		return nil
	})
	var seen []int
	ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		seen = append(seen, len(data.(*hook.ListPayload).Where))
		return nil
	})
	for _, q := range []string{"limit=2", "limit=2&cursor="} {
		if code, _, body := sweepCursorPage(t, ch, q); code != http.StatusOK {
			t.Fatalf("?%s = %d %s", q, code, body)
		}
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 1 {
		t.Fatalf("AfterList saw Where lengths %v, want [1 1] (offset, cursor)", seen)
	}
}

// A one-column cursor takes only the single-field encoding: a composite
// token would widen the keyset WHERE past the declared key.
func TestSingleFieldCursorRefusesComposite(t *testing.T) {
	multi := pagination.EncodeMultiCursor([]string{"id", "seq"}, map[string]any{"id": "a", "seq": 1})
	if _, err := decodeCursorAny(multi, []string{"id"}); err == nil {
		t.Fatal("a one-column cursor accepted a composite token")
	}
	if _, err := decodeCursorAny(pagination.EncodeCursor("id", "a"), []string{"id"}); err != nil {
		t.Fatalf("control: the single-field token is refused: %v", err)
	}
}

// Every list page is per-caller data and is never stored by a cache.
func TestReadPagesAreNoStore(t *testing.T) {
	ch, _ := covItems(t, seqCursor, 3)
	for _, q := range []string{"limit=2", "limit=2&cursor=", "stream=true"} {
		req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?"+q, nil), "u1")
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		if got := rec.Header().Get("Cache-Control"); rec.Code != http.StatusOK || got != "no-store" {
			t.Errorf("?%s = %d, Cache-Control %q, want 200 no-store", q, rec.Code, got)
		}
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items", nil), "u1")
	rec := httptest.NewRecorder()
	ch.ServeStreamingList(req.Context(), rec, req, []string{"id"}, nil, nil, nil, 1, 10, nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("direct stream Cache-Control %q, want no-store", got)
	}
}

// The stream's ?trashed= gate reads the request's user, not the ctx a
// direct caller passes for the database work.
func TestStreamTrashedGateReadsRequestUser(t *testing.T) {
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, seq INTEGER NOT NULL, name TEXT, deleted_at TIMESTAMP)`)
	ent := entity.Define("items", entity.EntityConfig{
		Name: "items", Table: "items",
		Fields: []schema.Field{{Name: "seq", Type: schema.Int, Required: true}, {Name: "name", Type: schema.String}},
		Scope:  &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	if _, err := db.Exec("INSERT INTO items (id, seq, name, deleted_at) VALUES ('a', 0, 'n', CURRENT_TIMESTAMP), ('b', 1, 'n', NULL)"); err != nil {
		t.Fatal(err)
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?trashed=true", nil), "u1")
	rec := httptest.NewRecorder()
	ch.ServeStreamingList(context.Background(), rec, req, []string{"id"}, nil, nil, nil, 1, 10, nil)
	var env struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if env.Total != 2 || len(env.Data) != 2 {
		t.Fatalf("trashed stream = total %d, %d rows, want 2 and 2: %s", env.Total, len(env.Data), rec.Body.String())
	}
}

// The cursor is taken before includes run: an include named like its
// foreign-key column replaces the stored value the next cursor needs.
func TestCursorTakenBeforeIncludes(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, author TEXT NOT NULL)`,
	)
	seedRows(t, db, "users", []map[string]any{{"id": "u1", "name": "a"}, {"id": "u2", "name": "b"}, {"id": "u3", "name": "c"}})
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "author": "u1"}, {"id": "p2", "author": "u2"}, {"id": "p3", "author": "u3"}})
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:     []schema.Field{{Name: "author", Type: schema.String, Required: true}},
		Relations:  []entity.Relation{entity.BelongsTo("author", "users", "author")},
		Pagination: &entity.PaginationConfig{CursorField: "author"},
	}.WithTimestamps(false))
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	get := func(cursor string) (int, pagination.CursorPage, string) {
		req := withTestUser(httptest.NewRequest(http.MethodGet, "/posts?limit=2&include=author&cursor="+url.QueryEscape(cursor), nil), "u1")
		rec := httptest.NewRecorder()
		ch.List()(rec, req)
		var page pagination.CursorPage
		_ = json.Unmarshal(rec.Body.Bytes(), &page)
		return rec.Code, page, rec.Body.String()
	}
	code, page, body := get("")
	if code != http.StatusOK || page.Cursor == "" {
		t.Fatalf("first page = %d %s", code, body)
	}
	if _, isObject := page.Data[0]["author"].(map[string]any); !isObject {
		t.Fatalf("include did not replace author: %s", body)
	}
	code, page, body = get(page.Cursor)
	if code != http.StatusOK || len(page.Data) != 1 || page.Data[0]["id"] != "p3" {
		t.Fatalf("second page = %d %s, want p3", code, body)
	}
}
