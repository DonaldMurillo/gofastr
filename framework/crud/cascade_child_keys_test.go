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

// childKeysHandler is posts with a cascade of every kind into notes;
// a note has line_2 (MarshalEntity derives "line2" for a Line2 field) and
// body_text.
func childKeysHandler(t *testing.T, jsonCase JSONCase) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, line_2 TEXT, body_text TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT)`,
		`CREATE TABLE post_notes (post_id TEXT NOT NULL, note_id TEXT NOT NULL, PRIMARY KEY (post_id, note_id))`,
	)
	public := &entity.ExposureConfig{Public: true}
	notes := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "post_id", Type: schema.String},
			{Name: "line_2", Type: schema.String},
			{Name: "body_text", Type: schema.String},
		},
		Exposure: public,
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
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
	ch := NewCrudHandler(posts, db).WithJSONCase(jsonCase)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}
	return ch, db
}

// A cascade child's keys are named the way its parent's are. In process,
// the parent body runs through bodyColumns, which reads the "line2" key
// MarshalEntity derives as line_2; the children folded through the HTTP
// rule, where "line2" is no column, and the field was dropped.
func TestInProcessCascadeChildNamesColumns(t *testing.T) {
	ch, db := childKeysHandler(t, CaseSnake)
	_, err := ch.CreateOne(context.Background(), map[string]any{
		"title":  "t",
		"lead":   map[string]any{"line2": "a"},
		"note":   map[string]any{"line2": "b"},
		"notes":  []any{map[string]any{"line2": "c"}},
		"tagged": []any{map[string]any{"line2": "d"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT COALESCE(line_2, '') FROM notes ORDER BY line_2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "a,b,c,d" {
		t.Fatalf("child line_2 values = %q, want a,b,c,d", got)
	}
}

// Two child keys folding onto one column ("bodyText" and "body_text" on a
// camel handler) are refused, as a batch item's are, instead of one
// winning by map order: a proxy that reads one of them would see a value
// the server may not write.
func TestCascadeChildFoldedKeysRefused(t *testing.T) {
	ch, db := childKeysHandler(t, CaseCamel)
	for _, body := range []string{
		`{"title":"t","note":{"bodyText":"a","body_text":"b"}}`,
		`{"title":"t","notes":[{"bodyText":"a","body_text":"b"}]}`,
		`{"title":"t","lead":{"bodyText":"a","body_text":"b"}}`,
		`{"title":"t","tagged":[{"bodyText":"a","body_text":"b"}]}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, r)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "body_text") {
			t.Errorf("%s = %d %s, want 400 naming body_text", body, rec.Code, rec.Body.String())
		}
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM notes) + (SELECT COUNT(*) FROM posts) + (SELECT COUNT(*) FROM post_notes)`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("the refused writes stored %d rows", n)
	}
}
