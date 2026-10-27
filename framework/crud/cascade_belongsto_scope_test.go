package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// readOnlyLeadHandler is posts with a belongs-to cascade onto notes whose
// ReadScope admits only status=published, and a ReadOnly posts.note_id: a
// client cannot choose the reference.
func readOnlyLeadHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, status TEXT, body TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT)`,
	)
	notes := entity.Define("notes", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "status", Type: schema.String},
			{Name: "body", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true, ReadScope: &entity.ReadScopeConfig{
			Filter: []entity.RowPredicate{{Field: "status", Value: "published"}},
		}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "note_id", Type: schema.String, ReadOnly: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("lead", "notes", "note_id").WithCascadeWrite(true),
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	notes.SetDB(db)
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"notes": notes, "posts": posts}}
	return ch, db
}

// A belongs-to cascade exists to point the parent at the child. When the
// write cannot set the parent's foreign key (ReadOnly here), the cascade
// would write a child the parent never references and hand it back as
// attached, and checkBelongsToScope, which reads only the references a
// write moves, never looked at it: a draft note came back whole. The
// cascade is refused before any child is written.
func TestBelongsToCascadeNeedsWritableKey(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		ch, db := readOnlyLeadHandler(t)
		seedRows(t, db, "notes", []map[string]any{
			{"id": "n1", "status": "published", "body": "one"},
			{"id": "n2", "status": "published", "body": "DRAFT-SECRET"},
		})
		seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "t", "note_id": "n1"}})
		r := httptest.NewRequest(http.MethodPatch, "/posts/p1",
			strings.NewReader(`{"lead":{"id":"n2","status":"draft"}}`))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("id", "p1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, r)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "DRAFT-SECRET") {
			t.Errorf("PATCH = %d %s, want 400 naming lead", rec.Code, rec.Body.String())
		}
		var fk, status string
		if err := db.QueryRow(`SELECT note_id FROM posts WHERE id = 'p1'`).Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT status FROM notes WHERE id = 'n2'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if fk != "n1" || status != "published" {
			t.Errorf("after PATCH: posts.note_id = %q, n2.status = %q; want n1 and published", fk, status)
		}
	})
	t.Run("create", func(t *testing.T) {
		ch, db := readOnlyLeadHandler(t)
		r := httptest.NewRequest(http.MethodPost, "/posts",
			strings.NewReader(`{"id":"p2","title":"t","lead":{"id":"n3","status":"draft","body":"DRAFT-SECRET"}}`))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, r)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "DRAFT-SECRET") {
			t.Errorf("POST = %d %s, want 400 naming lead", rec.Code, rec.Body.String())
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("POST left %d note(s) behind, want 0", n)
		}
	})
}

// The owner column is stamped on create and never moved by an update, so
// a belongs-to cascade through it links on create and is refused on
// update: re-pointing it there would write a person the post never
// references.
func TestBelongsToCascadeOwnerKeyOnUpdate(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t,
		`CREATE TABLE people (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, owner_id TEXT)`,
	)
	people := entity.Define("people", entity.EntityConfig{
		Table:    "people",
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("owner", "people", "owner_id").WithCascadeWrite(true)},
		Scope:     &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	people.SetDB(db)
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"people": people, "posts": posts}}
	seedRows(t, db, "people", []map[string]any{{"id": "u1", "name": "one"}})
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "t", "owner_id": "u1"}})

	r := httptest.NewRequest(http.MethodPatch, "/posts/p1", strings.NewReader(`{"owner":{"id":"u2","name":"two"}}`))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, withTestUser(r, "u1"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "cascade_key_not_writable") {
		t.Errorf("PATCH = %d %s, want 400 cascade_key_not_writable on owner", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM people`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("PATCH left %d people, want only u1", n)
	}
}

// The belongs-to arm hands back what the dependent arms do: a child the
// caller's read scope hides comes back as its id only. The parent write
// refuses a reference onto such a child afterwards, so this holds the arm
// itself to the rule rather than leaning on that later check.
func TestBelongsToCascadeChildHonorsReadScope(t *testing.T) {
	ch, _ := cascadeNotesHandler(t, CaseSnake, "")
	ctx := context.Background()
	tx, err := ch.DB.(*sql.DB).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	txh := *ch
	txh.DB = tx
	r := httptest.NewRequest(http.MethodPost, "/posts", nil)
	got, err := txh.processBelongsToCascadeWrites(ctx, r,
		map[string]any{"lead": map[string]any{"status": "draft", "body": "DRAFT-SECRET"}}, false)
	if err != nil {
		t.Fatalf("cascade: %v", err)
	}
	lead, _ := got["lead"].(map[string]any)
	if len(lead) != 1 || lead["id"] == nil {
		t.Errorf("belongs-to child = %v, want only its id", got["lead"])
	}
}
