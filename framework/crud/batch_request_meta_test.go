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

// The batch routes answer with the caller's rows, as the single-row
// routes do, and carry the request into the write, as the single-row
// routes do. They set neither: a shared cache could store one caller's
// batch result, and the audit hooks, which read the client IP and user
// agent from the request CRUD attaches, recorded batch writes with no
// request at all.
func TestBatchRoutesMatchSingleRow(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "parent_id", Type: schema.String},
	}, func(c *entity.EntityConfig) {
		c.Exposure = &entity.ExposureConfig{Public: true}
		c.Relations = []entity.Relation{entity.BelongsTo("parent", "posts", "parent_id")}
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, parent_id TEXT)`)
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "seed"}})

	type auditMeta struct{ agent, form string }
	seen := map[hook.HookType]auditMeta{}
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": ch.Entity}}
	ch.Hooks = hook.NewHookRegistry()
	ch.ChildHooks = func(string) *hook.HookRegistry { return ch.Hooks }
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		return data.(*hook.GetPayload).Request.ParseForm()
	})
	for _, ht := range []hook.HookType{hook.AfterCreate, hook.AfterUpdate, hook.AfterDelete} {
		ht := ht
		ch.Hooks.RegisterHook(ht, func(ctx context.Context, _ any) error {
			if r := AuditRequestFromContext(ctx); r != nil {
				seen[ht] = auditMeta{agent: r.UserAgent(), form: r.Form.Get("audit")}
			}
			return nil
		})
	}
	routes := []struct {
		name   string
		method string
		body   string
		serve  http.HandlerFunc
		hook   hook.HookType
	}{
		{"create", http.MethodPost, `{"items":[{"title":"a","parent_id":"p1"}]}`, ch.BatchCreate(), hook.AfterCreate},
		{"update", http.MethodPatch, `{"items":[{"id":"p1","title":"b"}]}`, ch.BatchUpdate(), hook.AfterUpdate},
		{"delete", http.MethodDelete, `{"ids":["p1"]}`, ch.BatchDelete(), hook.AfterDelete},
	}
	for _, rt := range routes {
		req := httptest.NewRequest(rt.method, "/posts/_batch?audit="+rt.name, strings.NewReader(rt.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "batch-agent/"+rt.name)
		rec := httptest.NewRecorder()
		rt.serve(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("batch %s = %d %s", rt.name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("batch %s Cache-Control = %q, want no-store", rt.name, got)
		}
		meta := seen[rt.hook]
		if got := meta.agent; got != "batch-agent/"+rt.name {
			t.Errorf("batch %s: %s hook saw request user agent %q, want batch-agent/%s", rt.name, rt.hook, got, rt.name)
		}
		if got := meta.form; got != rt.name {
			t.Errorf("batch %s: %s hook saw parsed audit form value %q, want %q", rt.name, rt.hook, got, rt.name)
		}
	}
}
