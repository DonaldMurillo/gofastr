package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestList_CountQueryErrorDoesNotLeakDriverText(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})
	_ = db.Close()

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts"})
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	body := strings.ToLower(rec.Body.String())
	if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
		t.Fatalf("SECURITY: [crud-error] list count leaked raw driver text: %s", rec.Body.String())
	}
}

func TestGet_QueryErrorDoesNotLeakDriverText(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})
	_ = db.Close()

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts/p1"})
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	body := strings.ToLower(rec.Body.String())
	if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
		t.Fatalf("SECURITY: [crud-error] get leaked raw driver text: %s", rec.Body.String())
	}
}

func TestList_AfterListHookErrorDoesNotLeakMessage(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		return errors.New("super-secret-hook-message")
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts"})
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if strings.Contains(rec.Body.String(), "super-secret-hook-message") {
		t.Fatalf("SECURITY: [crud-error] after-list hook leaked internal error text: %s", rec.Body.String())
	}
}

func TestGet_AfterGetHookErrorDoesNotLeakMessage(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("super-secret-hook-message")
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts/p1"})
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	if strings.Contains(rec.Body.String(), "super-secret-hook-message") {
		t.Fatalf("SECURITY: [crud-error] after-get hook leaked internal error text: %s", rec.Body.String())
	}
}
