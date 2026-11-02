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

// hookPanicSecret stands in for what a faulting hook holds: a DSN, a
// query, request bytes. None of it is written for the client.
const hookPanicSecret = "postgres://admin:hunter2@10.0.0.7/app"

// TestBeforeHookPanicIsAServerError: a Before hook's returned error is a
// refusal and answers 400 with its text, but a Before hook that PANICS has
// faulted. It answered 400 carrying "hook panic: <value>", so the panic
// value (here a DSN) reached the client and the fault read as the caller's
// mistake. Every write surface that runs a Before hook answers 500 with the
// generic body instead.
func TestBeforeHookPanicIsAServerError(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hkpanic (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("hkpanic", "hkpanic", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	ent := entity.Define("hkpanic", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	if _, err := db.Exec(`INSERT INTO hkpanic (id, user_id, name) VALUES ('r1', 'alice', 'x')`); err != nil {
		t.Fatal(err)
	}
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	for _, ht := range []hook.HookType{hook.BeforeCreate, hook.BeforeUpdate, hook.BeforeDelete} {
		ch.Hooks.RegisterHook(ht, func(context.Context, any) error { panic(hookPanicSecret) })
	}

	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		h       http.HandlerFunc
		id      string
		wantMin int // the status the surface answers a fault with
	}{
		{"create", http.MethodPost, "/hkpanic", `{"name":"n"}`, ch.Create(), "", http.StatusInternalServerError},
		{"update", http.MethodPatch, "/hkpanic/r1", `{"name":"n"}`, ch.Update(), "r1", http.StatusInternalServerError},
		{"delete", http.MethodDelete, "/hkpanic/r1", "", ch.Delete(), "r1", http.StatusInternalServerError},
		// A batch answers its envelope 400 (not committed) whatever an item
		// failed with; the item's own error text is the surface here.
		{"batch-create", http.MethodPost, "/hkpanic/_batch", `{"items":[{"name":"n"}]}`, ch.BatchCreate(), "", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := makeRequest(t, RequestOpts{Method: c.method, Path: c.path, Body: c.body, UserID: "alice"})
			if c.id != "" {
				req.SetPathValue("id", c.id)
			}
			rr := httptest.NewRecorder()
			c.h(rr, req)
			body := rr.Body.String()
			if rr.Code != c.wantMin {
				t.Errorf("status = %d, want %d; body %s", rr.Code, c.wantMin, body)
			}
			if strings.Contains(body, "hunter2") || strings.Contains(body, "hook panic") {
				t.Errorf("SECURITY: a Before hook's panic value reached the client: %s", body)
			}
		})
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hkpanic`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows = %d, want 1: a panicking Before hook must stop the write", n)
	}
}

// TestHookPanicIsNotARefusal: IsHookRefusal is how an in-process caller
// (entityui's state override) tells the caller's 400 from a server fault.
// A panic is a fault.
func TestHookPanicIsNotARefusal(t *testing.T) {
	err := &beforeHookError{err: &hook.PanicError{Value: "boom"}}
	if IsHookRefusal(err) {
		t.Error("IsHookRefusal reported a hook panic as the hook refusing the write")
	}
	if !IsHookRefusal(&beforeHookError{err: context.Canceled}) {
		t.Error("IsHookRefusal no longer reports a returned hook error")
	}
}

// TestReadHookPanicIsAServerError is the read side of
// TestBeforeHookPanicIsAServerError: List and Get answered a panicking
// BeforeList/BeforeGet with 400 and "hook panic: <value>" (MCP read tools
// re-dispatch through the same routes), and a child entity's panicking
// BeforeList reached through ?include= or ?rel.field= did the same. A read
// hook's panic answers the generic 500; a returned error stays a 400.
func TestReadHookPanicIsAServerError(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hkrpanic (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("hkrpanic", "hkrpanic", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	ent := entity.Define("hkrpanic", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	if _, err := db.Exec(`INSERT INTO hkrpanic (id, user_id, name) VALUES ('r1', 'alice', 'x')`); err != nil {
		t.Fatal(err)
	}
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	for _, ht := range []hook.HookType{hook.BeforeList, hook.BeforeGet} {
		ch.Hooks.RegisterHook(ht, func(context.Context, any) error { panic(hookPanicSecret) })
	}
	check := func(t *testing.T, code int, body string) {
		t.Helper()
		if code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500; body %s", code, body)
		}
		if strings.Contains(body, "hunter2") || strings.Contains(body, "hook panic") {
			t.Errorf("SECURITY: a read hook's panic value reached the client: %s", body)
		}
	}
	for _, c := range []struct {
		name, path, id string
		h              http.HandlerFunc
	}{
		{"list", "/hkrpanic", "", ch.List()},
		{"get", "/hkrpanic/r1", "r1", ch.Get()},
	} {
		t.Run(c.name, func(t *testing.T) {
			req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: c.path, UserID: "alice"})
			if c.id != "" {
				req.SetPathValue("id", c.id)
			}
			rr := httptest.NewRecorder()
			c.h(rr, req)
			check(t, rr.Code, rr.Body.String())
		})
	}

	notesHooks := hook.NewHookRegistry()
	notesHooks.RegisterHook(hook.BeforeList, func(context.Context, any) error { panic(hookPanicSecret) })
	projects, _ := projectNotes(t, map[string]*hook.HookRegistry{"notes": notesHooks})
	for _, target := range []string{"/projects?include=notes", "/projects?notes.body=x"} {
		t.Run("child "+target, func(t *testing.T) {
			code, body := listBody(t, projects, target)
			check(t, code, body)
		})
	}
}
