package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A has_one or has_many cascade points each child at its parent by
// writing the child's foreign key, and the child's create skips a ReadOnly
// or Hidden field (or one the child does not declare). The children were
// stored with a NULL post_id while the response showed them attached
// (2026-10-27 D34). The cascade is refused before any row is written, as
// the belongs_to arm already refuses a key the parent cannot set.
func TestCascadeRefusesUnwritableChildLink(t *testing.T) {
	for name, fk := range map[string]*schema.Field{
		"hidden":     {Name: "post_id", Type: schema.String, Hidden: true},
		"read only":  {Name: "post_id", Type: schema.String, ReadOnly: true},
		"undeclared": nil,
	} {
		t.Run(name, func(t *testing.T) {
			db := setupDB(t,
				`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
				`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`,
			)
			fields := []schema.Field{
				{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
				{Name: "body", Type: schema.String},
			}
			if fk != nil {
				fields = append(fields, *fk)
			}
			public := &entity.ExposureConfig{Public: true}
			notes := entity.Define("notes", entity.EntityConfig{Fields: fields, Exposure: public}.WithTimestamps(false))
			posts := entity.Define("posts", entity.EntityConfig{
				Fields: []schema.Field{
					{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
					{Name: "title", Type: schema.String},
				},
				Relations: []entity.Relation{
					entity.HasOne("note", "notes", "post_id").WithCascadeWrite(true),
					entity.HasMany("notes", "notes", "post_id").WithCascadeWrite(true),
				},
				Exposure: public,
			}.WithTimestamps(false))
			ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
			ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}

			// An update that creates a child (a has_one with none yet, a
			// has_many item with no id) takes the same refusal.
			seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "kept"}})
			for _, body := range []string{
				`{"title":"t","note":{"body":"one"}}`,
				`{"title":"t","notes":[{"body":"one"},{"body":"two"}]}`,
			} {
				for _, method := range []string{http.MethodPost, http.MethodPatch} {
					target, handle := "/posts", ch.Create()
					if method == http.MethodPatch {
						target, handle = "/posts/p1", ch.Update()
					}
					r := httptest.NewRequest(method, target, strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.SetPathValue("id", "p1")
					rec := httptest.NewRecorder()
					handle(rec, r)
					if rec.Code < 400 || !strings.Contains(rec.Body.String(), "cascade_key_not_writable") {
						t.Errorf("%s %s = %d %s, want the unwritable link refused", method, body, rec.Code, rec.Body.String())
					}
				}
			}
			var notesN, postsN int
			var title string
			if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM notes), (SELECT COUNT(*) FROM posts), (SELECT title FROM posts WHERE id = 'p1')`).Scan(&notesN, &postsN, &title); err != nil {
				t.Fatal(err)
			}
			if notesN != 0 || postsN != 1 || title != "kept" {
				t.Errorf("after refused cascades: %d notes, %d posts, p1 title %q; want 0, 1, kept", notesN, postsN, title)
			}
		})
	}
}

// A writable link still attaches: the refusal is about the column, not
// about cascades into notes.
func TestCascadeWritableChildLinkAttaches(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	notes := entity.Define("notes", entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
		{Name: "post_id", Type: schema.String},
		{Name: "body", Type: schema.String},
	}, Exposure: public}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{entity.HasMany("notes", "notes", "post_id").WithCascadeWrite(true)},
		Exposure:  public,
	}.WithTimestamps(false))
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}
	got := postCascade(t, ch, `{"title":"t","notes":[{"body":"one"}]}`)
	var linked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes WHERE post_id = ?`, got["id"]).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("notes linked to the new post = %d (%v), want 1", linked, err)
	}
}
