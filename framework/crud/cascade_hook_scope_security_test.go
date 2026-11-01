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
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// hookScopedNotesHandler is cascadeNotesHandler with the notes read scope
// written as BeforeList/BeforeGet hooks (published only) instead of a
// declared ReadScope: the documented other way to scope reads.
func hookScopedNotesHandler(t *testing.T) (*CrudHandler, *sql.DB) {
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
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "post_id", Type: schema.String},
			{Name: "status", Type: schema.String},
			{Name: "body", Type: schema.String},
		},
		Exposure: public,
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
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"notes": notes, "posts": posts}}
	noteHooks := hook.NewHookRegistry()
	noteHooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("status = $1", "published")
		return nil
	})
	noteHooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		data.(*hook.GetPayload).AddWhere("status = $1", "published")
		return nil
	})
	ch.ChildHooks = func(name string) *hook.HookRegistry {
		if name == "notes" {
			return noteHooks
		}
		return nil
	}
	return ch, db
}

func sendPosts(ch *CrudHandler, method, path, id, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if id != "" {
		r.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	if method == http.MethodPost {
		ch.Create()(rec, r)
	} else {
		ch.Update()(rec, r)
	}
	return rec
}

// A cascade item reaching a child row its own BeforeList scope hides
// (which ?include= honours) linked it with a 201 where a missing child
// answers 404, an existence oracle, and an item naming it with one more
// field updated it and echoed every stored column (2026-11-01 bug bash).
// A hidden child now answers what a missing one does, on every arm that
// names an existing child.
func TestCascadeHonoursChildBeforeHookScope(t *testing.T) {
	ch, db := hookScopedNotesHandler(t)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n-draft", "post_id": "p0", "status": "draft", "body": "DRAFT-SECRET"},
		{"id": "n-pub", "post_id": "p1", "status": "published", "body": "PUB"},
	})
	seedRows(t, db, "posts", []map[string]any{{"id": "p0", "title": "seed"}, {"id": "p1", "title": "seed"}})

	// Precondition: ?include= hides the draft.
	gr := httptest.NewRecorder()
	greq := httptest.NewRequest(http.MethodGet, "/posts/p0?include=notes", nil)
	greq.SetPathValue("id", "p0")
	ch.Get()(gr, greq)
	if gr.Code != http.StatusOK || strings.Contains(gr.Body.String(), "DRAFT-SECRET") {
		t.Fatalf("precondition: include = %d %s", gr.Code, gr.Body.String())
	}

	cases := []struct {
		name, method, id, body string
	}{
		{"m2m bare-id link", http.MethodPost, "", `{"title":"t","tagged":["%s"]}`},
		{"m2m item update", http.MethodPost, "", `{"title":"t","tagged":[{"id":"%s","status":"draft"}]}`},
		{"has_many item update", http.MethodPut, "p0", `{"title":"t","notes":[{"id":"%s","body":"x"}]}`},
		{"has_one item update", http.MethodPut, "p0", `{"title":"t","note":{"id":"%s","body":"x"}}`},
		{"belongs_to reference", http.MethodPost, "", `{"title":"t","note_id":"%s"}`},
	}
	for _, c := range cases {
		path := "/posts"
		if c.id != "" {
			path += "/" + c.id
		}
		missing := sendPosts(ch, c.method, path, c.id, strings.ReplaceAll(c.body, "%s", "no-such-note"))
		hidden := sendPosts(ch, c.method, path, c.id, strings.ReplaceAll(c.body, "%s", "n-draft"))
		if hidden.Code != missing.Code || hidden.Code != http.StatusNotFound {
			t.Errorf("SECURITY: %s: a hook-hidden note = %d, a missing one = %d, want both 404 (%s)",
				c.name, hidden.Code, missing.Code, hidden.Body.String())
		}
		if strings.Contains(hidden.Body.String(), "DRAFT-SECRET") {
			t.Errorf("SECURITY: %s handed back a hook-hidden note: %s", c.name, hidden.Body.String())
		}
	}
	// A has_one item with no id updates the parent's existing child; one
	// the caller cannot see is not the caller's to overwrite.
	if rec := sendPosts(ch, http.MethodPut, "/posts/p0", "p0", `{"title":"t","note":{"body":"OVERWRITE"}}`); strings.Contains(rec.Body.String(), "DRAFT-SECRET") {
		t.Errorf("SECURITY: a has_one update handed back a hook-hidden note: %s", rec.Body.String())
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM notes WHERE id = 'n-draft'`).Scan(&body); err != nil || body != "DRAFT-SECRET" {
		t.Errorf("SECURITY: a cascade write changed a hook-hidden note: body = %q (%v)", body, err)
	}

	// A visible note still links and updates, and comes back whole.
	rec := sendPosts(ch, http.MethodPost, "/posts", "", `{"title":"t","tagged":[{"id":"n-pub","body":"PUB2"}],"note_id":"n-pub"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "PUB2") {
		t.Fatalf("a visible note: %d %s, want 201 with the updated row", rec.Code, rec.Body.String())
	}
}

// A child a cascade write creates, or moves, out of its own BeforeList
// scope comes back as its id alone, as one its declared ReadScope hides
// does (TestCascadeChildHonorsReadScope).
func TestCascadeReadBackHonoursChildBeforeHookScope(t *testing.T) {
	ch, db := hookScopedNotesHandler(t)
	seedRows(t, db, "notes", []map[string]any{{"id": "n-pub", "status": "published", "body": "WAS-PUBLIC"}})
	draft := `{"status":"draft","body":"DRAFT-SECRET"}`
	got := postCascade(t, ch, `{"title":"t","note":`+draft+`,"notes":[`+draft+`],"tagged":[`+draft+
		`,{"id":"n-pub","status":"draft"}]}`)
	tagged, _ := got["tagged"].([]any)
	children := map[string]any{"note": got["note"], "notes": firstOf(t, got["notes"])}
	if len(tagged) == 2 {
		children["tagged created"], children["tagged moved"] = tagged[0], tagged[1]
	} else {
		t.Fatalf("tagged = %v, want two items", got["tagged"])
	}
	for rel, c := range children {
		if m, ok := c.(map[string]any); !ok || len(m) != 1 || m["id"] == nil {
			t.Errorf("SECURITY: %s: cascade child = %v, want only its id", rel, c)
		}
	}
	pub := `{"status":"published","body":"PUB"}`
	got = postCascade(t, ch, `{"title":"t","note":`+pub+`,"notes":[`+pub+`]}`)
	if m, _ := got["note"].(map[string]any); m["body"] != "PUB" {
		t.Fatalf("published cascade child = %v, want the full row", got["note"])
	}
}
