package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A BeforeList / BeforeGet scope narrows the HTTP list and get. A screen
// reading through WithReadHooks renders to the same caller, so it must
// see the same rows: the in-process reads run the before hooks too.
func scopeToOpen(ch *CrudHandler) {
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("status = $1", "open")
		return nil
	})
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		data.(*hook.GetPayload).AddWhere("status = $1", "open")
		return nil
	})
}

func TestReadHooksApplyBeforeScopes(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open"},
		map[string]any{"id": "a2", "user_id": "alice", "status": "closed"},
	)
	scopeToOpen(ch)
	ctx := WithReadHooks(ctxWithUser("alice"))

	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "a1" {
		t.Errorf("SECURITY: ListAll ignored BeforeList: %v", rows)
	}
	n, err := ch.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("SECURITY: CountAll ignored BeforeList: %d, want 1", n)
	}
	if _, err := ch.GetOne(ctx, "a2", nil); !errors.Is(err, errNotFound) {
		t.Errorf("SECURITY: GetOne ignored BeforeGet: err = %v, want not found", err)
	}
	if _, err := ch.GetOne(ctx, "a1", nil); err != nil {
		t.Errorf("GetOne in scope: %v", err)
	}
}

// The typed query runs BeforeList under the opt-in, for Find, First and
// Count alike.
func TestTypedQueryAppliesBeforeList(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open"},
		map[string]any{"id": "a2", "user_id": "alice", "status": "closed"},
	)
	scopeToOpen(ch)
	type inv struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	ctx := WithReadHooks(ctxWithUser("alice"))
	rows, err := NewTypedQuery[inv](ch).Find(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "a1" {
		t.Errorf("SECURITY: Find ignored BeforeList: %d rows", len(rows))
	}
	n, err := NewTypedQuery[inv](ch).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("SECURITY: Count ignored BeforeList: %d", n)
	}
	first, err := NewTypedQuery[inv](ch).Where(entity.NewStringColumn("id").Eq("a2")).First(ctx)
	if err == nil {
		t.Errorf("SECURITY: First returned %v past BeforeList", first)
	}
}

// Without the opt-in the in-process API stays a system read: stored
// values, no hook scopes, so a hook that reads its own entity cannot
// recurse and read-modify-write still finds the row.
func TestBeforeScopesNeedReadHooks(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open"},
		map[string]any{"id": "a2", "user_id": "alice", "status": "closed"},
	)
	scopeToOpen(ch)
	ctx := ctxWithUser("alice")
	if rows, _ := ch.ListAll(ctx, ListOptions{}); len(rows) != 2 {
		t.Errorf("system ListAll = %d rows, want 2", len(rows))
	}
	if _, err := ch.GetOne(ctx, "a2", nil); err != nil {
		t.Errorf("system GetOne: %v", err)
	}
}

// A before hook that errors refuses the read, as it answers 400 over HTTP.
func TestBeforeScopeErrorRefusesRead(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open"})
	ch.Hooks.RegisterHook(hook.BeforeList, func(context.Context, any) error { return errors.New("no team") })
	ch.Hooks.RegisterHook(hook.BeforeGet, func(context.Context, any) error { return errors.New("no team") })
	ctx := WithReadHooks(ctxWithUser("alice"))
	if _, err := ch.ListAll(ctx, ListOptions{}); err == nil {
		t.Error("ListAll read past a failing BeforeList")
	}
	if _, err := ch.CountAll(ctx, ListOptions{}); err == nil {
		t.Error("CountAll read past a failing BeforeList")
	}
	if _, err := ch.GetOne(ctx, "a1", nil); err == nil {
		t.Error("GetOne read past a failing BeforeGet")
	}
}

// A before hook that reads its own entity through the context it was
// handed gets a system read, not itself again.
func TestBeforeScopeHookDoesNotRecurse(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open"})
	calls := 0
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		calls++
		if calls > 3 {
			return errors.New("recursed")
		}
		_, err := ch.ListAll(ctx, ListOptions{})
		if err == nil {
			_, err = ch.ListAll(data.(*hook.ListPayload).Request.Context(), ListOptions{})
		}
		return err
	})
	if _, err := ch.ListAll(WithReadHooks(ctxWithUser("alice")), ListOptions{}); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if calls != 1 {
		t.Errorf("BeforeList ran %d times, want 1", calls)
	}
}

// ReadHooksRestrict hands a hook the same system-read context the reads
// do: a hook that reads its own entity through its request's context
// gets a system read, not itself again.
func TestReadHooksRestrictDoesNotRecurse(t *testing.T) {
	ch, _ := setupInvoiceWorld(t)
	calls := 0
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		calls++
		if calls > 3 {
			return errors.New("recursed")
		}
		_, err := ch.ListAll(data.(*hook.ListPayload).Request.Context(), ListOptions{})
		return err
	})
	if ch.ReadHooksRestrict(ctxWithUser("alice"), "gone") {
		t.Error("a hook appending no clause restricted the caller")
	}
	if calls != 1 {
		t.Errorf("BeforeList ran %d times, want 1", calls)
	}
}

// ReadHooksRestrict runs the hooks on the request the context carries,
// as the in-process reads do, so a hook that branches on a header sees
// the caller's.
func TestReadHooksRestrictSeesRealRequest(t *testing.T) {
	ch, _ := setupInvoiceWorld(t)
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		p := data.(*hook.GetPayload)
		if p.Request.Header.Get("X-Role") != "auditor" {
			p.Where = append(p.Where, hook.WhereClause{SQL: "user_id = ?", Args: []any{"nobody"}})
		}
		return nil
	})
	r := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	r.Header.Set("X-Role", "auditor")
	if ch.ReadHooksRestrict(withRealRequest(ctxWithUser("alice"), r), "gone") {
		t.Error("the hook restricted a caller its request header clears")
	}
	if !ch.ReadHooksRestrict(ctxWithUser("alice"), "gone") {
		t.Error("the hook did not restrict a caller with no header")
	}
}
