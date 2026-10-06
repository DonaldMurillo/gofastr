package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// elevatedReq is a member holding none of docs' permissions, elevated.
func elevatedReq(r *http.Request) *http.Request {
	r = grantReq(r, "unrelated:thing")
	return r.WithContext(WithElevation(r.Context()))
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
	ctx := WithElevation(grantReq(httptest.NewRequest(http.MethodGet, "/", nil), "unrelated:thing").Context())
	if !ch.CanReadScoped(ctx) || !ch.CanReadRecordScoped(ctx, "d1") {
		t.Fatal("elevated caller: CanReadScoped = false, want true")
	}
	if !ch.CanCreateScoped(ctx) || !ch.CanUpdateRecordScoped(ctx, "d1") || !ch.CanDeleteRecordScoped(ctx, "d1") {
		t.Fatal("elevated caller: a write gate answered false, want true")
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
	ctx := WithElevation(ctxWithGrant(signedIn("alice"), "unrelated:thing"))
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
	ctx := WithElevation(ctxWithGrant(ctxWithUser("u1"), "unrelated:thing"))
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
