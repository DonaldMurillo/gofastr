package crud

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// elevatedReq is a member holding none of docs' permissions, elevated.
func elevatedReq(r *http.Request) *http.Request {
	r = grantReq(r, "unrelated:thing")
	return r.WithContext(WithElevation(r.Context(), "docs"))
}

// An elevated caller holding none of the entity's Access permissions
// reads, creates, updates and deletes.
func TestElevationLiftsAccess(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','one')`); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name   string
		method string
		id     string
		body   string
		h      http.HandlerFunc
		want   int
	}{
		{"list", http.MethodGet, "", "", ch.List(), http.StatusOK},
		{"get", http.MethodGet, "d1", "", ch.Get(), http.StatusOK},
		{"create", http.MethodPost, "", `{"body":"two"}`, ch.Create(), http.StatusCreated},
		{"update", http.MethodPatch, "d1", `{"body":"uno"}`, ch.Update(), http.StatusOK},
		{"delete", http.MethodDelete, "d1", "", ch.Delete(), http.StatusNoContent},
	}
	for _, s := range steps {
		req := elevatedReq(httptest.NewRequest(s.method, "/api/docs", strings.NewReader(s.body)))
		req.Header.Set("Content-Type", "application/json")
		if s.id != "" {
			req.SetPathValue("id", s.id)
		}
		rec := httptest.NewRecorder()
		s.h(rec, req)
		if rec.Code != s.want {
			t.Fatalf("elevated %s = %d, want %d. body=%s", s.name, rec.Code, s.want, rec.Body.String())
		}
	}
	ctx := WithElevation(grantReq(httptest.NewRequest(http.MethodGet, "/", nil), "unrelated:thing").Context(), "docs")
	if !ch.CanReadScoped(ctx) || !ch.CanReadRecordScoped(ctx, "d1") {
		t.Fatal("elevated caller: CanReadScoped = false, want true")
	}
	if !ch.CanCreateScoped(ctx) || !ch.CanUpdateRecordScoped(ctx, "d1") || !ch.CanDeleteRecordScoped(ctx, "d1") {
		t.Fatal("elevated caller: a write gate answered false, want true")
	}
}

// Elevation lifts only the entities it names: one named for another
// entity, or naming none, leaves docs' Access in force, and
// WithoutElevation drops the whole set.
func TestElevationNamesItsEntities(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','one')`); err != nil {
		t.Fatal(err)
	}
	base := grantReq(httptest.NewRequest(http.MethodGet, "/", nil), "unrelated:thing").Context()
	for name, ctx := range map[string]context.Context{
		"another entity": WithElevation(base, "posts"),
		"no entity":      WithElevation(base),
		"stripped":       WithoutElevation(WithElevation(base, "docs")),
	} {
		if ch.CanReadScoped(ctx) || ch.CanReadRecordScoped(ctx, "d1") || ch.CanCreateScoped(ctx) || ch.CanDeleteRecordScoped(ctx, "d1") {
			t.Errorf("SECURITY: elevation for %s lifted docs' Access", name)
		}
	}
	if !ch.CanReadScoped(WithElevation(base, "posts", "docs")) {
		t.Fatal("elevation naming docs among others did not lift docs' Access")
	}
}

// A hook runs as the caller: the elevated write and read go through, but
// the hook's own context, and the request a read payload carries, no
// longer lift the entity's Access.
func TestElevationSkipsHooks(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','one')`); err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	lifted := map[string]bool{}
	note := func(name string) hook.HookFunc {
		return func(ctx context.Context, data any) error {
			lifted[name] = lifted[name] || ch.CanReadScoped(ctx)
			if p, ok := data.(*hook.GetPayload); ok && p.Request != nil {
				lifted[name+" request"] = ch.CanReadScoped(p.Request.Context())
			}
			return nil
		}
	}
	for _, typ := range []hook.HookType{hook.BeforeUpdate, hook.AfterUpdate, hook.BeforeGet, hook.AfterGet} {
		ch.Hooks.RegisterHook(typ, note(fmt.Sprint(typ)))
	}
	upd := elevatedReq(httptest.NewRequest(http.MethodPatch, "/api/docs/d1", strings.NewReader(`{"body":"uno"}`)))
	upd.Header.Set("Content-Type", "application/json")
	upd.SetPathValue("id", "d1")
	get := elevatedReq(httptest.NewRequest(http.MethodGet, "/api/docs/d1", nil))
	get.SetPathValue("id", "d1")
	for _, step := range []struct {
		r    *http.Request
		h    http.HandlerFunc
		want int
	}{{upd, ch.Update(), http.StatusOK}, {get, ch.Get(), http.StatusOK}} {
		rec := httptest.NewRecorder()
		step.h(rec, step.r)
		if rec.Code != step.want {
			t.Fatalf("elevated %s = %d, want %d: %s", step.r.Method, rec.Code, step.want, rec.Body.String())
		}
	}
	if len(lifted) != 6 {
		t.Fatalf("setup: the hooks that ran = %v, want four hooks and two payload requests", lifted)
	}
	for name, ok := range lifted {
		if ok {
			t.Errorf("SECURITY: %s ran with the back office's elevation", name)
		}
	}
}

// A Decider's deny still refuses an elevated caller.
func TestElevationKeepsDeciderDeny(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','one')`); err != nil {
		t.Fatal(err)
	}
	cd := &capturingDecider{ret: access.DecisionDeny}
	req := reqWithDecider(elevatedReq(httptest.NewRequest(http.MethodPatch, "/api/docs/d1", strings.NewReader(`{"body":"x"}`))), cd.fn)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "d1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: elevated update under a denying Decider = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	if cd.cap != "docs:write" || cd.ref.ID != "d1" {
		t.Fatalf("decider asked about %q %+v, want docs:write on d1", cd.cap, cd.ref)
	}
	if ch.CanReadScoped(req.Context()) {
		t.Fatal("SECURITY: elevated CanReadScoped under a denying Decider = true")
	}
}

// Elevation grants no cross-owner read: an elevated caller on an
// owner-scoped entity still sees only their own rows.
func TestElevationKeepsOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t) // alice t-a, bob t-b
	ctx := WithElevation(ctxWithGrant(signedIn("alice"), "unrelated:thing"), "ctickets")
	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 1 || rows[0]["user_id"] != "alice" {
		t.Fatalf("SECURITY: elevated list = %v, want only alice's row", rows)
	}
	if row, err := ch.GetOne(ctx, "t-b", nil); err == nil {
		t.Fatalf("SECURITY: elevated GetOne of bob's row = %v, want not found", row)
	}
}

// A move's own Permission is not lifted: elevation answers the entity's
// update permission, never the move's.
func TestElevationKeepsMovePermission(t *testing.T) {
	ch, db := statesPermittedWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	ctx := WithElevation(ctxWithGrant(ctxWithUser("u1"), "unrelated:thing"), "invoices")
	_, err := ch.RunTransition(ctx, "i1", "pay")
	if err == nil || !strings.Contains(err.Error(), "missing permission invoices:pay") {
		t.Fatalf("SECURITY: elevated move without invoices:pay: err = %v, want a denial naming it", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("SECURITY: elevated move changed the row to %q", status)
	}
	// The update permission alone was lifted: a move with no Permission runs.
	if _, err := ch.RunTransition(ctx, "i1", "void"); err != nil {
		t.Fatalf("elevated move needing only update: %v", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "void" {
		t.Fatalf("elevated void stored status %q, want void", status)
	}
}
