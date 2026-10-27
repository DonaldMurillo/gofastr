package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// cascadeNotesHandler builds posts with a cascade write of every relation
// kind onto notes, whose ReadScope admits only status=published. noteKey,
// when set, is the WireName of the notes primary key.
func cascadeNotesHandler(t *testing.T, jsonCase JSONCase, noteKey string) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, status TEXT, body TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT)`,
		`CREATE TABLE post_notes (post_id TEXT NOT NULL, note_id TEXT NOT NULL, PRIMARY KEY (post_id, note_id))`,
	)
	public := &entity.ExposureConfig{Public: true}
	notes := entity.Define("notes", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true, WireName: noteKey},
			{Name: "post_id", Type: schema.String},
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
			{Name: "title", Type: schema.String},
			{Name: "note_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("lead", "notes", "note_id").WithCascadeWrite(true),
			entity.HasOne("note", "notes", "post_id").WithCascadeWrite(true),
			entity.HasMany("notes", "notes", "post_id").WithCascadeWrite(true),
			entity.ManyToMany("tagged", "notes", "post_notes", "post_id", "note_id").WithCascadeWrite(true),
		},
		Exposure: public,
	}.WithTimestamps(false))
	notes.SetDB(db)
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db).WithJSONCase(jsonCase)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"notes": notes, "posts": posts}}
	return ch, db
}

func postCascade(t *testing.T, ch *CrudHandler, body string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /posts = %d %s", rec.Code, rec.Body.String())
	}
	return decodeSingleResponse(t, rec.Body.Bytes())
}

// A cascade child is a row of the child entity handed to the caller: a
// draft note a GET answers 404 for must come back as its id only, on every
// dependent relation kind, while a published one comes back whole. A
// belongs-to child never gets here hidden: checkBelongsToScope refuses the
// parent's FK onto it first.
func TestCascadeChildHonorsReadScope(t *testing.T) {
	ch, _ := cascadeNotesHandler(t, CaseSnake, "")
	draft := `{"status":"draft","body":"DRAFT-SECRET"}`
	got := postCascade(t, ch, `{"title":"t","note":`+draft+
		`,"notes":[`+draft+`],"tagged":[`+draft+`]}`)
	children := map[string]any{
		"note":   got["note"],
		"notes":  firstOf(t, got["notes"]),
		"tagged": firstOf(t, got["tagged"]),
	}
	for rel, c := range children {
		m, ok := c.(map[string]any)
		if !ok || len(m) != 1 || m["id"] == nil {
			t.Errorf("%s: cascade child = %v, want only its id", rel, c)
		}
	}
	pub := `{"status":"published","body":"PUB"}`
	got = postCascade(t, ch, `{"title":"t","note":`+pub+`}`)
	if m, _ := got["note"].(map[string]any); m["body"] != "PUB" {
		t.Fatalf("published cascade child = %v, want the full row", got["note"])
	}
}

// A many-to-many item naming a visible note with more fields updates it;
// an update that moves it out of the read scope hides it too.
func TestCascadeM2MUpdateHonorsReadScope(t *testing.T) {
	ch, db := cascadeNotesHandler(t, CaseSnake, "")
	seedRows(t, db, "notes", []map[string]any{{"id": "n1", "status": "published", "body": "SECRET"}})
	got := postCascade(t, ch, `{"title":"t","tagged":[{"id":"n1","status":"draft"}]}`)
	if m, _ := firstOf(t, got["tagged"]).(map[string]any); len(m) != 1 || m["id"] != "n1" {
		t.Fatalf("tagged = %v, want only {id: n1}", got["tagged"])
	}
}

// The child's key is read under the CHILD's wire key: a notes key with a
// WireName left the belongs-to FK and the pivot link nil.
func TestCascadeChildKeyWireName(t *testing.T) {
	ch, db := cascadeNotesHandler(t, CaseCamel, "noteKey")
	pub := `{"status":"published","body":"PUB"}`
	got := postCascade(t, ch, `{"title":"t","lead":`+pub+`,"tagged":[`+pub+`]}`)
	lead, _ := got["lead"].(map[string]any)
	if got["noteId"] == nil || got["noteId"] != lead["noteKey"] {
		t.Fatalf("posts.note_id = %v, want the lead note's key %v", got["noteId"], lead["noteKey"])
	}
	var linked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM post_notes WHERE note_id IS NOT NULL AND note_id <> ''`).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("pivot links with a note key = %d (%v), want 1", linked, err)
	}
	// Linking by key answers under the same wire key.
	got = postCascade(t, ch, `{"title":"t","tagged":[{"noteKey":"`+lead["noteKey"].(string)+`"}]}`)
	if m, _ := firstOf(t, got["tagged"]).(map[string]any); m["noteKey"] != lead["noteKey"] {
		t.Fatalf("linked note = %v, want {noteKey: %v}", got["tagged"], lead["noteKey"])
	}
}

func firstOf(t *testing.T, v any) any {
	t.Helper()
	s, ok := v.([]any)
	if !ok || len(s) == 0 {
		t.Fatalf("relation = %v, want a non-empty list", v)
	}
	return s[0]
}
