package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// hookCalls records every id a BeforeUpdate or BeforeDelete hook saw.
type hookCalls struct {
	mu  sync.Mutex
	ids []string
}

func (c *hookCalls) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ids = append(c.ids, s)
}

func (c *hookCalls) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.ids
	c.ids = nil
	return out
}

// beforeOrderWorld is an owner-scoped, tenant-scoped, soft-delete entity
// with recording BeforeUpdate and BeforeDelete hooks. alice in tenant t1
// owns n-a; bob owns n-b; n-t sits in tenant t2 under alice; n-x is
// alice's trashed row.
func beforeOrderWorld(t *testing.T, db *sql.DB) (*CrudHandler, *hookCalls) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE bnotes (id TEXT PRIMARY KEY, user_id TEXT, tenant_id TEXT, title TEXT, deleted_at TIMESTAMP)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	ent := entity.Define("bnotes", entity.EntityConfig{Table: "bnotes",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Scope: &entity.ScopeConfig{OwnerField: "user_id", MultiTenant: true, SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	calls := &hookCalls{}
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.BeforeUpdate, func(_ context.Context, data any) error {
		calls.add("update")
		return nil
	})
	reg.RegisterHook(hook.BeforeDelete, func(_ context.Context, data any) error {
		calls.add("delete:" + data.(string))
		return nil
	})
	ch.Hooks = reg
	pgSeed(t, db, "bnotes", []map[string]any{
		{"id": "n-a", "user_id": "alice", "tenant_id": "t1", "title": "A"},
		{"id": "n-b", "user_id": "bob", "tenant_id": "t1", "title": "B"},
		{"id": "n-t", "user_id": "alice", "tenant_id": "t2", "title": "T"},
	})
	if _, err := db.Exec(`INSERT INTO bnotes (id, user_id, tenant_id, title, deleted_at) VALUES ('n-x', 'alice', 't1', 'X', CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("seed trashed: %v", err)
	}
	return ch, calls
}

// A BeforeUpdate or BeforeDelete hook never sees an id the caller cannot
// write: another owner's row, another tenant's, a trashed row, a missing
// id. The write answers the 404 it always did, and the hook, which may
// send an email or delete a file no rollback undoes, is never called.
// The caller's own row still runs the hooks.
func TestBeforeWriteHooksSkipUntouchableIDs(t *testing.T) {
	installOwnerExtractor(t)
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		ch, calls := beforeOrderWorld(t, db)
		alice := tenant.SetTenantID(signedIn("alice"), "t1")
		for _, id := range []string{"n-b", "n-t", "n-x", "n-missing"} {
			if _, err := ch.UpdateOne(alice, id, map[string]any{"title": "hijack"}); !errors.Is(err, errNotFound) {
				t.Errorf("UpdateOne(%s) err = %v, want errNotFound", id, err)
			}
			if err := ch.DeleteOne(alice, id); !errors.Is(err, errNotFound) {
				t.Errorf("DeleteOne(%s) err = %v, want errNotFound", id, err)
			}
			if _, err := ch.BatchUpdateMany(alice, []string{id}, []map[string]any{{"title": "hijack"}}); err == nil {
				t.Errorf("BatchUpdateMany(%s) succeeded", id)
			}
			if _, err := ch.BatchDeleteMany(alice, []string{id}); err == nil {
				t.Errorf("BatchDeleteMany(%s) succeeded", id)
			}

			put := httptest.NewRequest(http.MethodPut, "/bnotes/"+id, strings.NewReader(`{"title":"hijack"}`))
			put.Header.Set("Content-Type", "application/json")
			put.SetPathValue("id", id)
			put = put.WithContext(alice)
			rec := httptest.NewRecorder()
			ch.Update()(rec, put)
			if rec.Code != http.StatusNotFound {
				t.Errorf("PUT %s status = %d, want 404: %s", id, rec.Code, rec.Body.String())
			}
			del := httptest.NewRequest(http.MethodDelete, "/bnotes/"+id, nil)
			del.SetPathValue("id", id)
			del = del.WithContext(alice)
			rec = httptest.NewRecorder()
			ch.Delete()(rec, del)
			if rec.Code != http.StatusNotFound {
				t.Errorf("DELETE %s status = %d, want 404: %s", id, rec.Code, rec.Body.String())
			}
			if got := calls.take(); len(got) != 0 {
				t.Errorf("Before hooks ran for untouchable id %s: %v", id, got)
			}
		}

		// The caller's own row: the hooks run, as before.
		if _, err := ch.UpdateOne(alice, "n-a", map[string]any{"title": "mine"}); err != nil {
			t.Fatalf("UpdateOne(own): %v", err)
		}
		if err := ch.DeleteOne(alice, "n-a"); err != nil {
			t.Fatalf("DeleteOne(own): %v", err)
		}
		if got := calls.take(); strings.Join(got, ",") != "update,delete:n-a" {
			t.Fatalf("hooks on own row = %v, want [update delete:n-a]", got)
		}
	})
}
