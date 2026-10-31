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

// AfterList and AfterGet rows carry wire keys, camelCase by default, so
// the redaction hooks-and-transactions.md shows deletes "internalNotes"
// (2026-10-27 bug bash W14: the doc deleted "internal_notes", which no
// row carries, and the value shipped).
func TestReadHooksSeeWireKeys(t *testing.T) {
	db := setupDB(t, `CREATE TABLE posts (id TEXT PRIMARY KEY, internal_notes TEXT)`)
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "internal_notes", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "internal_notes": "NOTE-SECRET"}})

	serve := func(t *testing.T, key string) (list, get string) {
		t.Helper()
		ch := NewCrudHandler(ent, db)
		reg := hook.NewHookRegistry()
		reg.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
			for _, row := range data.(*hook.ListPayload).Results {
				delete(row, key)
			}
			return nil
		})
		reg.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
			delete(data.(*hook.GetPayload).Result, key)
			return nil
		})
		ch.Hooks = reg
		rec := httptest.NewRecorder()
		ch.List()(rec, httptest.NewRequest(http.MethodGet, "/posts", nil))
		list = rec.Body.String()
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/posts/p1", nil)
		req.SetPathValue("id", "p1")
		ch.Get()(rec, req)
		return list, rec.Body.String()
	}

	list, get := serve(t, "internalNotes")
	for name, body := range map[string]string{"list": list, "get": get} {
		if strings.Contains(body, "NOTE-SECRET") || !strings.Contains(body, "p1") {
			t.Errorf("%s: deleting the wire key did not redact: %s", name, body)
		}
	}
	// The column name is not a key the hook sees.
	list, get = serve(t, "internal_notes")
	if !strings.Contains(list, "NOTE-SECRET") || !strings.Contains(get, "NOTE-SECRET") {
		t.Errorf("deleting the column name changed the response; the doc's casing note is wrong: list %s get %s", list, get)
	}
}
