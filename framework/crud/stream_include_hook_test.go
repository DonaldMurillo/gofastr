package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// streamPostsHandler builds a "posts" entity with an "author" relation so
// ?include=author parses as a valid include.
func streamPostsHandler(t *testing.T) (*CrudHandler, *registryStub) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, author_id TEXT, title TEXT, secret TEXT)`,
		`CREATE TABLE authors (id TEXT PRIMARY KEY, name TEXT)`)
	cfg := entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "author_id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "secret", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "authors", "author_id"),
		},
	}.WithTimestamps(false)
	ent := entity.Define("posts", cfg)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	reg := &registryStub{ents: map[string]*entity.Entity{"posts": ent}}
	ch.Registry = reg
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "author_id": "a1", "title": "hi", "secret": "topsecret"},
	})
	return ch, reg
}

type registryStub struct{ ents map[string]*entity.Entity }

func (r *registryStub) All() map[string]*entity.Entity { return r.ents }
func (r *registryStub) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r.ents))
	for _, e := range r.ents {
		out = append(out, e)
	}
	return out
}
func (r *registryStub) Get(name string) (*entity.Entity, error) {
	if e, ok := r.ents[name]; ok {
		return e, nil
	}
	return nil, errNotFound
}

// TestStreamWithIncludeRejected pins that ?stream=true together with
// ?include= is refused with 400 rather than silently dropping the include.
func TestStreamWithIncludeRejected(t *testing.T) {
	ch, _ := streamPostsHandler(t)
	req := httptest.NewRequest("GET", "/posts?stream=true&include=author", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stream+include status = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
}

// TestStreamWithAfterListRejected pins that streaming an entity carrying an
// AfterList redactor is refused (400) rather than emitting un-redacted rows.
func TestStreamWithAfterListRejected(t *testing.T) {
	ch, _ := streamPostsHandler(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p := data.(*hook.ListPayload)
		for i := range p.Results {
			delete(p.Results[i], "secret")
		}
		return nil
	})

	req := httptest.NewRequest("GET", "/posts?stream=true", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stream+AfterList status = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	// The redactor must never be bypassed: the secret must not appear in the body.
	if strings.Contains(rec.Body.String(), "topsecret") {
		t.Fatalf("AfterList redaction bypassed by streaming: secret leaked in body=%s", rec.Body.String())
	}
}
