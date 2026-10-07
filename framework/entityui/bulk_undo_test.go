package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A bulk delete on a soft-deleting entity toasts Undo when its bar asks,
// and the Undo restores what the run deleted, under the caller's own
// gates and scope.

// liveNotesUI is the notes app with ids live and nothing trashed.
func liveNotesUI(t *testing.T, ids ...string) *testUI {
	t.Helper()
	installOwnerExtractor(t)
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, map[string]any{"id": id, "name": "note " + id, "status": "open", "deleted_at": nil})
	}
	return newTestUI(t,
		map[string]entity.EntityConfig{"notes": notesConfig()},
		map[string][]map[string]any{"notes": rows},
		withAPI(map[string]string{"notes": "/api/notes"}),
		withSingleConn(),
	)
}

// postNotesBulk posts body to the notes bulk handler as ctx's caller.
func postNotesBulk(t *testing.T, x *testUI, ctx context.Context, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return postNotesBulkRaw(x, ctx, string(raw))
}

func postNotesBulkRaw(x *testUI, ctx context.Context, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/notes/_bulk", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.ui.BulkHandler("notes").ServeHTTP(rec, req)
	return rec
}

// toastUndo is the action on the answer's one toast, or nil.
func toastUndo(t *testing.T, rec *httptest.ResponseRecorder) *interactive.ToastAction {
	t.Helper()
	var toasts []ui.ToastTrigger
	if err := json.Unmarshal([]byte(rec.Header().Get("X-Gofastr-Toast")), &toasts); err != nil || len(toasts) != 1 {
		t.Fatalf("want one toast, got %q (%v)", rec.Header().Get("X-Gofastr-Toast"), err)
	}
	return toasts[0].Action
}

// liveNoteIDs reads the notes not in the trash, sorted.
func liveNoteIDs(t *testing.T, x *testUI) []string {
	t.Helper()
	rows, err := x.db.Query(`SELECT id FROM notes WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func deleteBody(ids ...string) map[string]any {
	return map[string]any{"action": "delete", "scope": "selected", "ids": ids, "undo": "1", "back": "/notes?sort=name"}
}

func TestBulkDeleteUndoRestores(t *testing.T) {
	x := liveNotesUI(t, "a", "b", "c")
	ctx := bulkCtx("u1", nil)
	rec := postNotesBulk(t, x, ctx, deleteBody("a", "b"))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rec.Code, rec.Body.String())
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("live after delete = %v, want [c]", got)
	}
	if h := rec.Header().Get("X-Gofastr-Toast"); !strings.Contains(h, `"title":"2 notes deleted"`) {
		t.Errorf("the delete's toast = %s, want 2 notes deleted", h)
	}
	undo := toastUndo(t, rec)
	if undo == nil {
		t.Fatal("the delete's toast carries no Undo")
	}
	if undo.Label != "Undo" || undo.Attrs["data-cui-rpc"] != "/api/notes/_bulk" ||
		undo.Attrs["data-cui-rpc-method"] != "POST" || undo.Attrs["data-cui-rpc-navigate"] != "/notes?sort=name" {
		t.Fatalf("Undo = %+v, want a POST to the bulk route that returns to the list", undo)
	}
	var body struct {
		Action, Scope string
		IDs           []string
	}
	if err := json.Unmarshal([]byte(undo.Attrs["data-cui-rpc-body"]), &body); err != nil {
		t.Fatal(err)
	}
	if body.Action != "restore" || body.Scope != "deleted" || !slices.Equal(body.IDs, []string{"a", "b"}) {
		t.Fatalf("Undo body = %+v, want a restore of a and b", body)
	}

	rec = postNotesBulkRaw(x, ctx, undo.Attrs["data-cui-rpc-body"])
	if rec.Code != http.StatusOK {
		t.Fatalf("Undo's restore = %d %s", rec.Code, rec.Body.String())
	}
	if h := rec.Header().Get("X-Gofastr-Toast"); !strings.Contains(h, `"2 notes restored"`) || strings.Contains(h, `"action"`) {
		t.Errorf("the restore's toast = %s, want 2 notes restored and no Undo", h)
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("live after Undo = %v, want all three back", got)
	}
}

// No Undo without the bar's ask, without a path on this origin to
// return to, or on an entity that deletes for good.
func TestBulkUndoOnlyWhenAsked(t *testing.T) {
	ctx := bulkCtx("u1", nil)
	for name, tune := range map[string]func(map[string]any){
		"not asked":    func(b map[string]any) { delete(b, "undo") },
		"no back":      func(b map[string]any) { delete(b, "back") },
		"offsite back": func(b map[string]any) { b["back"] = "//evil.example/notes" },
	} {
		x := liveNotesUI(t, "a")
		body := deleteBody("a")
		tune(body)
		rec := postNotesBulk(t, x, ctx, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: bulk delete = %d %s", name, rec.Code, rec.Body.String())
		}
		if a := toastUndo(t, rec); a != nil {
			t.Errorf("%s: the toast offers Undo: %+v", name, a)
		}
	}

	x := ownedInvoices(t, Extensions{})
	raw, _ := json.Marshal(map[string]any{"action": "delete", "scope": "selected", "ids": invoiceIDs(t, x)[:1], "undo": "1", "back": "/invoices"})
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/_bulk", strings.NewReader(string(raw))).WithContext(bulkCtx("u1", nil))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.ui.BulkHandler("invoices").ServeHTTP(rec, req)
	if strings.Contains(rec.Header().Get("X-Gofastr-Toast"), `"action"`) {
		t.Errorf("a hard delete offered Undo: %s", rec.Header().Get("X-Gofastr-Toast"))
	}
}

// The ids ride the toast header, so a run too big for the budget offers
// no Undo rather than answering headers a proxy refuses.
func TestBulkUndoOverBudgetDropped(t *testing.T) {
	ids := make([]string, 60)
	for i := range ids {
		ids[i] = fmt.Sprintf("note-%036d", i)
	}
	x := liveNotesUI(t, ids...)
	rec := postNotesBulk(t, x, bulkCtx("u1", nil), deleteBody(ids...))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rec.Code, rec.Body.String())
	}
	if a := toastUndo(t, rec); a != nil {
		t.Fatalf("a %d-id Undo rode the header", len(ids))
	}
	if n := len(rec.Header().Get("X-Gofastr-Toast")); n > undoActionBudget {
		t.Fatalf("toast header is %d bytes", n)
	}
}

// A caller who may delete but not update gets no Undo: the restore
// would refuse every row.
func TestBulkUndoNeedsUpdate(t *testing.T) {
	x, policy := ownerNotesUI(t)
	if _, err := x.db.Exec(`UPDATE notes SET deleted_at = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("deleter", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	rec := postNotesBulk(t, x, bulkCtx("u1", policy, "deleter"), deleteBody("u1-dead"))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rec.Code, rec.Body.String())
	}
	if a := toastUndo(t, rec); a != nil {
		t.Fatalf("a caller without update was offered Undo: %+v", a)
	}
}

// Undo names only the rows the run deleted: a row whose delete the
// Decider refused stays live and out of the restore.
func TestBulkUndoNamesDeletedRowsOnly(t *testing.T) {
	x, policy := ownerNotesUI(t)
	if _, err := x.db.Exec(`UPDATE notes SET deleted_at = NULL, user_id = 'u1'`); err != nil {
		t.Fatal(err)
	}
	deny := func(_ context.Context, _ []string, p access.Permission, r access.Ref) access.Decision {
		if r.ID == "u2-dead" && p == "notes:delete" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	ctx := access.WithDecider(bulkCtx("u1", policy, "editor"), deny)
	rec := postNotesBulk(t, x, ctx, deleteBody("u1-dead", "u2-dead"))
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rec.Code, rec.Body.String())
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"u2-dead"}) {
		t.Fatalf("live = %v, want the refused row only", got)
	}
	// A run that skipped a row counts each outcome.
	if h := rec.Header().Get("X-Gofastr-Toast"); !strings.Contains(h, "1 done, 1 skipped, 0 failed") {
		t.Errorf("the toast = %s, want the counts", h)
	}
	undo := toastUndo(t, rec)
	if undo == nil {
		t.Fatal("no Undo for the row that went")
	}
	var body struct{ IDs []string }
	if err := json.Unmarshal([]byte(undo.Attrs["data-cui-rpc-body"]), &body); err != nil || !slices.Equal(body.IDs, []string{"u1-dead"}) {
		t.Fatalf("Undo restores %v (%v), want [u1-dead]", body.IDs, err)
	}
}

// The restore re-reads the posted ids from the trash under the caller's
// scope and gates each write, so a forged body restores nothing the
// caller could not restore one at a time.
func TestBulkRestoreScopedToCaller(t *testing.T) {
	x, policy := ownerNotesUI(t)
	rec := postNotesBulk(t, x, bulkCtx("u2", policy, "editor"),
		map[string]any{"action": "restore", "scope": "deleted", "ids": []string{"u1-dead", "u2-dead"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"u2-dead"}) {
		t.Fatalf("SECURITY: live after u2's restore = %v, want only u2's row", got)
	}

	x, policy = ownerNotesUI(t)
	if err := policy.Grant("deleter", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	rec = postNotesBulk(t, x, bulkCtx("u1", policy, "deleter"),
		map[string]any{"action": "restore", "scope": "deleted", "ids": []string{"u1-dead"}})
	if got := liveNoteIDs(t, x); len(got) != 0 {
		t.Fatalf("SECURITY: a caller without update restored %v (%d %s)", got, rec.Code, rec.Body.String())
	}
}

// Restore runs on the deleted scope only, and the deleted scope runs
// restore only, at most InRequestCap ids.
func TestBulkRestoreScopeBounds(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	ctx := bulkCtx("u1", nil)
	for name, body := range map[string]map[string]any{
		"restore on a live scope": {"action": "restore", "scope": "selected", "ids": []string{"n1"}},
		"delete on the trash":     {"action": "delete", "scope": "deleted", "ids": []string{"n2"}},
	} {
		if rec := postNotesBulk(t, x, ctx, body); rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d %s, want 403", name, rec.Code, rec.Body.String())
		}
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"n1"}) {
		t.Fatalf("a refused run changed the rows: live = %v", got)
	}
	over := make([]string, InRequestCap+1)
	for i := range over {
		over[i] = fmt.Sprintf("gone-%d", i)
	}
	over[0] = "n2"
	if rec := postNotesBulk(t, x, ctx, map[string]any{"action": "restore", "scope": "deleted", "ids": over}); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), fmt.Sprintf("at most %d", InRequestCap)) {
		t.Errorf("an over-cap restore = %d %s, want 422 naming the cap", rec.Code, rec.Body.String())
	}
	if got := liveNoteIDs(t, x); !slices.Equal(got, []string{"n1"}) {
		t.Fatalf("an over-cap restore ran: live = %v", got)
	}

	orders := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/orders/_bulk",
		strings.NewReader(`{"action":"restore","scope":"deleted","ids":["o1"]}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	orders.ui.BulkHandler("orders").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("restore on a hard-deleting entity = %d, want 403", rec.Code)
	}
}

// The bar asks for Undo, with the list to return to, only under Undo()
// on a soft-deleting entity.
func TestBulkBarAsksUndo(t *testing.T) {
	x := liveNotesUI(t, "a", "b")
	html := listHTML(t, x.ui.List("notes").Bulk().Undo(), x.userCtx("/notes", "?sort=name", "u1"))
	for _, want := range []string{`name="undo" type="hidden" value="1"`, `name="back" type="hidden" value="/notes?sort=name"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the bar under Undo() is missing %s:\n%s", want, html)
		}
	}
	if html := listHTML(t, x.ui.List("notes").Bulk(), x.userCtx("/notes", "?sort=name", "u1")); strings.Contains(html, `name="undo"`) {
		t.Error("the bar asks for Undo without Undo()")
	}
}
