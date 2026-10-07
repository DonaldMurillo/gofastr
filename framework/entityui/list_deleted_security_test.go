package entityui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The trash view's security posture: another owner's trashed row is
// neither listed nor restorable nor purgeable, and the two writes ask
// the caller's own update and delete permission about the record.

// ownerNotesUI is a soft-deleting, owner-scoped notes app with update
// and delete gated by permissions, seeded with u1's trashed row.
func ownerNotesUI(t *testing.T) (*testUI, *access.RolePolicy) {
	t.Helper()
	installOwnerExtractor(t)
	cfg := notesConfig()
	cfg.Scope = &entity.ScopeConfig{SoftDelete: true, OwnerField: "user_id"}
	cfg.Fields = fields(
		schema.Field{Name: "user_id", Type: schema.String, Required: true},
		schema.Field{Name: "name", Type: schema.String, Required: true},
		schema.Field{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}},
	)
	cfg.Exposure = &entity.ExposureConfig{Public: true, Access: entity.AccessControl{
		Update: "notes:update", Delete: "notes:delete",
	}}
	cfg = cfg.WithTimestamps(false)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"notes": cfg},
		map[string][]map[string]any{"notes": {
			{"id": "u1-dead", "user_id": "u1", "name": "u1 trashed", "status": "open", "deleted_at": "2026-01-01T00:00:00Z"},
			{"id": "u2-dead", "user_id": "u2", "name": "u2 trashed", "status": "open", "deleted_at": "2026-01-01T00:00:00Z"},
		}},
		withAPI(map[string]string{"notes": "/api/notes"}),
		withSingleConn(),
	)
	policy := access.NewRolePolicy()
	policy.Register("notes:update", "notes:delete")
	// The full-trash role: both writes allowed, so the cross-owner and
	// live-row tests measure scope, not permission.
	if err := policy.Grant("editor", "notes:update", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	return x, policy
}

func TestDeletedListHidesOtherOwnersTrash(t *testing.T) {
	x, policy := ownerNotesUI(t)
	ctx := access.WithRoles(access.WithPolicy(x.userCtx("/notes", "?view=deleted", "u2"), policy), []string{"editor"})
	html := listHTML(t, x.ui.List("notes").Deleted(), ctx)
	if !strings.Contains(html, "u2 trashed") {
		t.Errorf("u2's own trashed row did not list:\n%s", html)
	}
	if strings.Contains(html, "u1 trashed") {
		t.Errorf("SECURITY: u2's trash view listed u1's trashed row:\n%s", html)
	}
}

func TestRestoreOtherOwnersRowRefused(t *testing.T) {
	x, policy := ownerNotesUI(t)
	r := httptest.NewRequest(http.MethodPost, "/api/notes/u1-dead/_restore", strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u2")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"editor"}))
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: u2 restoring u1's row answered %d, want 404", w.Code)
	}
	// The row is untouched: u1 still sees it in their trash view.
	ctx := access.WithRoles(access.WithPolicy(x.userCtx("/notes", "?view=deleted", "u1"), policy), []string{"editor"})
	if !strings.Contains(listHTML(t, x.ui.List("notes").Deleted(), ctx), "u1 trashed") {
		t.Errorf("u2's refused restore changed u1's row")
	}
}

func TestPurgeOtherOwnersRowRefused(t *testing.T) {
	x, policy := ownerNotesUI(t)
	r := httptest.NewRequest(http.MethodPost, "/api/notes/u1-dead/_purge", strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u2")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"editor"}))
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: u2 purging u1's row answered %d, want 404", w.Code)
	}
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = ? AND deleted_at IS NOT NULL`, "u1-dead").Scan(&n); err != nil || n != 1 {
		t.Errorf("u2's refused purge removed or revived u1's row (count=%d err=%v)", n, err)
	}
}

func TestRestoreNeedsUpdatePermission(t *testing.T) {
	x, policy := ownerNotesUI(t)
	// deleter holds delete only: update is deliberately withheld.
	if err := policy.Grant("deleter", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	// The row draws no Restore control for this caller.
	ctx := access.WithRoles(access.WithPolicy(x.userCtx("/notes", "?view=deleted", "u1"), policy), []string{"deleter"})
	html := listHTML(t, x.ui.List("notes").Deleted(), ctx)
	if strings.Contains(html, "_restore") {
		t.Errorf("a caller without update access drew Restore:\n%s", html)
	}
	// And the handler refuses the write itself, row untouched.
	r := httptest.NewRequest(http.MethodPost, "/api/notes/u1-dead/_restore", strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"deleter"}))
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("restore without update permission answered %d, want 403", w.Code)
	}
	after := access.WithRoles(access.WithPolicy(x.userCtx("/notes", "?view=deleted", "u1"), policy), []string{"deleter"})
	if !strings.Contains(listHTML(t, x.ui.List("notes").Deleted(), after), "u1 trashed") {
		t.Errorf("a refused restore changed the row")
	}
}

func TestPurgeNeedsDeletePermission(t *testing.T) {
	x, policy := ownerNotesUI(t)
	// updater holds update only: delete is deliberately withheld.
	if err := policy.Grant("updater", "notes:update"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/notes/u1-dead/_purge", strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"updater"}))
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("purge without delete permission answered %d, want 403", w.Code)
	}
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = ?`, "u1-dead").Scan(&n); err != nil || n != 1 {
		t.Errorf("a refused purge deleted the row (count=%d err=%v)", n, err)
	}
}

// TestPurgeLiveRowAnswers409AndKeepsRow pins that purge can never be
// turned into a hard delete of a live row.
func TestPurgeLiveRowAnswers409AndKeepsRow(t *testing.T) {
	x, policy := ownerNotesUI(t)
	if err := policy.Grant("editor", "notes:update", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.db.Exec(`INSERT INTO notes (id, user_id, name, status, deleted_at) VALUES ('u1-live', 'u1', 'u1 live', 'open', NULL)`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/notes/u1-live/_purge", strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"editor"}))
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("purge of a live row answered %d, want 409", w.Code)
	}
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'u1-live'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("SECURITY: a refused purge hard-deleted the live row (count=%d err=%v)", n, err)
	}
}
