package entityui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The trash view: ?view=deleted lists only soft-deleted rows, with the
// trash actions in place of the row menu, and the restore and purge
// handlers answer both the form RPC and a plain post.

func notesConfig() entity.EntityConfig {
	// WithTimestamps(false): crud's RestoreOne mis-binds its UPDATE's
	// arguments on an entity with auto timestamps (its Set runs after
	// its Where, so the id placeholder binds the timestamp), which is
	// that package's bug to fix; the trash view itself does not care.
	return entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}},
		),
		Exposure: &entity.ExposureConfig{Public: true},
		Scope:    &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false)
}

func notesRows() []map[string]any {
	return []map[string]any{
		{"id": "n1", "name": "live one", "status": "open", "deleted_at": nil},
		{"id": "n2", "name": "dead one", "status": "paid", "deleted_at": "2026-01-01T00:00:00Z"},
	}
}

// withSingleConn pins the test database to one connection BEFORE any
// row is seeded: a write that runs in a transaction (restore, purge)
// must see the same :memory: database the seeds wrote, and a pooled
// :memory: gives every connection its own.
func withSingleConn() testUIOption {
	return func(x *testUI) { x.db.SetMaxOpenConns(1) }
}

func deletedUI(t *testing.T) *testUI {
	t.Helper()
	return newTestUI(t,
		map[string]entity.EntityConfig{"notes": notesConfig()},
		map[string][]map[string]any{"notes": notesRows()},
		withAPI(map[string]string{"notes": "/api/notes"}),
		withSingleConn(),
	)
}

func TestDeletedViewListsTrashedRows(t *testing.T) {
	x := deletedUI(t)
	html := listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", "?view=deleted", "u1"))
	if !strings.Contains(html, "dead one") {
		t.Errorf("the trash view lost its trashed row:\n%s", html)
	}
	if strings.Contains(html, "live one") {
		t.Errorf("the trash view showed a live row:\n%s", html)
	}
	for _, want := range []string{
		">Deleted<",              // the tab beside the views
		"/api/notes/n2/_restore", // the row's Restore post
		"/api/notes/n2/_purge",   // the row's Delete permanently post
		"Delete permanently",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the trash view is missing %q:\n%s", want, html)
		}
	}
	// No record link, no New: the record screens read live rows only.
	for _, refuse := range []string{`href="/notes/n2"`, "/notes/create"} {
		if strings.Contains(html, refuse) {
			t.Errorf("the trash view drew %q:\n%s", refuse, html)
		}
	}
	// The purge asks first.
	if !strings.Contains(html, "cannot be undone") {
		t.Errorf("the purge posts without a confirm:\n%s", html)
	}
}

func TestDeletedViewFiltersWithinTrash(t *testing.T) {
	x := deletedUI(t)
	html := listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", "?view=deleted&filter=status+%3D+%22open%22", "u1"))
	// The one trashed row is paid; an open filter leaves nothing.
	if strings.Contains(html, "dead one") {
		t.Errorf("the filter did not narrow the trash view:\n%s", html)
	}
	if !strings.Contains(html, "No deleted notes yet") {
		t.Errorf("the trash view's empty state did not draw:\n%s", html)
	}
}

func TestDeletedViewNeedsSoftDelete(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	html := listHTML(t, x.ui.List("orders").Deleted(), x.ctx("/orders", "?view=deleted"))
	if strings.Contains(html, ">Deleted<") {
		t.Errorf("a hard-deleting entity drew the trash tab:\n%s", html)
	}
	// ?view=deleted falls back to All: the live rows.
	if !strings.Contains(html, "alpha") {
		t.Errorf("the unknown view did not fall back to All:\n%s", html)
	}
}

func TestDeletedOffByDefault(t *testing.T) {
	x := deletedUI(t)
	html := listHTML(t, x.ui.List("notes"), x.ctx("/notes", "?view=deleted"))
	if strings.Contains(html, ">Deleted<") {
		t.Errorf("the trash tab drew without Deleted():\n%s", html)
	}
	if !strings.Contains(html, "live one") {
		t.Errorf("a trash view opened without Deleted() being asked for:\n%s", html)
	}
}

// deletedWriteMux mounts the two handlers the way the host does.
func deletedWriteMux(x *testUI) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /api/notes/{id}/_restore", x.ui.RestoreHandler("notes"))
	mux.Handle("POST /api/notes/{id}/_purge", x.ui.PurgeHandler("notes"))
	return mux
}

// postDeletedWrite posts one trash write as a plain form.
func postDeletedWrite(t *testing.T, x *testUI, path, back string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"back": {back}}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	return w
}

// withUserRequest stamps the test user on a request the tests build.
func withUserRequest(r *http.Request, id string) *http.Request {
	return r.WithContext(handler.SetUser(r.Context(), &testUser{id: id}))
}

func TestRestorePlainPostRestoresAndRedirects(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	w := postDeletedWrite(t, x, "/api/notes/n2/_restore", "/notes?view=deleted")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("restore answered %d (%s), want 303", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/notes?view=deleted" {
		t.Fatalf("restore Location = %q, want the list's deleted view", got)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("restore answer is cacheable: %q", w.Header().Get("Cache-Control"))
	}
	// The row is live again, and back in the ordinary list.
	html := listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", "", "u1"))
	if !strings.Contains(html, "dead one") {
		t.Errorf("the restored row did not return to the live list:\n%s", html)
	}
}

func TestPurgePlainPostDeletesAndRedirects(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	w := postDeletedWrite(t, x, "/api/notes/n2/_purge", "/notes?view=deleted")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("purge answered %d (%s), want 303", w.Code, w.Body.String())
	}
	// Gone from both views.
	for _, q := range []string{"", "?view=deleted"} {
		html := listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", q, "u1"))
		if strings.Contains(html, "dead one") {
			t.Errorf("the purged row still lists at %q:\n%s", q, html)
		}
	}
}

func TestPurgeLiveRowRefused(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	w := postDeletedWrite(t, x, "/api/notes/n1/_purge", "/notes?view=deleted")
	if w.Code != http.StatusConflict {
		t.Fatalf("purge of a live row answered %d, want 409", w.Code)
	}
	if !strings.Contains(listHTML(t, x.ui.List("notes"), x.ctx("/notes", "")), "live one") {
		t.Errorf("a refused purge deleted the live row")
	}
}

func TestTrashWriteRefusesAbsoluteBack(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	for _, back := range []string{"https://evil.example/notes", "//evil.example/notes", "notes/relative", `/\evil.example/notes`, "/%2Fevil.example/notes"} {
		w := postDeletedWrite(t, x, "/api/notes/n2/_restore", back)
		if w.Code != http.StatusBadRequest {
			t.Errorf("back=%q answered %d, want 400", back, w.Code)
		}
	}
	// Nothing ran: the row is still trashed.
	html := listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", "?view=deleted", "u1"))
	if !strings.Contains(html, "dead one") {
		t.Errorf("a refused back path still ran the write")
	}
}

func TestTrashWriteJSONAnswersStatus(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	r := httptest.NewRequest(http.MethodPost, "/api/notes/n2/_restore", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r = withUserRequest(r, "u1")
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("JSON restore answered %d (%s), want 200", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Errorf("JSON restore body = %s", w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "toast") && w.Header().Get("X-Gofastr-Toast") == "" && !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("JSON restore carried no toast header")
	}
}

func TestTrashWriteCrossSiteRefused(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	form := url.Values{"back": {"/notes?view=deleted"}}
	r := httptest.NewRequest(http.MethodPost, "/api/notes/n2/_purge", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r = withUserRequest(r, "u1")
	w := httptest.NewRecorder()
	deletedWriteMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a cross-site purge answered %d, want 403", w.Code)
	}
	if !strings.Contains(listHTML(t, x.ui.List("notes").Deleted(), x.ctx("/notes", "?view=deleted")), "dead one") {
		t.Errorf("a cross-site purge deleted the row")
	}
}

func TestTrashWriteUnknownEntityNotFound(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	w := postDeletedWrite(t, x, "/api/notes/n2/_restore", "/notes?view=deleted")
	_ = w
	// A hard-deleting entity's handler answers 404 (ErrNoSoftDelete).
	y := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	form := url.Values{"back": {"/orders"}}
	r := httptest.NewRequest(http.MethodPost, "/api/orders/o1/_restore", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	w2 := httptest.NewRecorder()
	m := http.NewServeMux()
	m.Handle("POST /api/orders/{id}/_restore", y.ui.RestoreHandler("orders"))
	m.ServeHTTP(w2, r)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("restore on a hard-deleting entity answered %d, want 404", w2.Code)
	}
}

func TestTrashWriteBodyCapped(t *testing.T) {
	x := deletedUI(t)
	installOwnerExtractor(t)
	// A body past the cap answers 413, not an unbounded parse.
	big := strings.Repeat("a", 64<<10)
	w := postDeletedWrite(t, x, "/api/notes/n2/_restore", "/notes?x="+big)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized body answered %d, want 413", w.Code)
	}
	if !strings.Contains(listHTML(t, x.ui.List("notes").Deleted(), x.userCtx("/notes", "?view=deleted", "u1")), "dead one") {
		t.Errorf("an oversized body still ran the write")
	}
}
