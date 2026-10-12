package entityui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Saved views' security posture: a view belongs to the caller who made
// it. Another user can neither open it (it reads as gone, the caller's
// own rows untouched) nor delete it.

func TestSavedViewForeignIDDoesNotApply(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "u1 secret open", Filter: `status = "open"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// u2 opens u1's id.
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved="+saved.ID, "u2"))
	if !strings.Contains(html, "This view no longer applies") {
		t.Errorf("a foreign saved id drew no callout:\n%s", html)
	}
	// u1's filter was not applied to u2's rows.
	if !strings.Contains(html, "zeta") {
		t.Errorf("SECURITY: u1's saved filter narrowed u2's list:\n%s", html)
	}
	// u1's view name is not revealed.
	if strings.Contains(html, "u1 secret open") {
		t.Errorf("SECURITY: u1's view name leaked to u2:\n%s", html)
	}
}

func TestSavedViewDeleteForeignRefused(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "u1 mine", Filter: `status = "open"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := postSavedViews(t, x, "u2", "/api/orders/_views/_delete/"+saved.ID, url.Values{"back": {"/orders"}})
	if w.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: u2 deleting u1's view answered %d, want 404", w.Code)
	}
	if _, err := store.Get(asUser(context.Background(), "u1"), "orders", saved.ID); err != nil {
		t.Errorf("SECURITY: u2's refused delete removed u1's view: %v", err)
	}
}

func TestSavedViewHiddenFieldRefusedAtSaveAndGoneAtOpen(t *testing.T) {
	x, store := savedUI(t)
	// At save: a filter naming an unknown or Hidden field answers 400.
	if w := postSavedViews(t, x, "u1", "/api/orders/_views", url.Values{
		"name": {"Nope"}, "filter": {"nosuchfield = 1"}, "back": {"/orders"},
	}); w.Code != http.StatusBadRequest {
		t.Errorf("a filter naming an unknown field answered %d, want 400", w.Code)
	}
	// If one reached the store anyway (an older save, a field that
	// became Hidden since), the open path still refuses it.
	store.put("u1", SavedView{Entity: "orders", Name: "Old", Filter: "nosuchfield = 1"})
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", "?saved=sv-1", "u1"))
	if !strings.Contains(html, "This view no longer applies") {
		t.Errorf("a store-side stale filter opened without the callout:\n%s", html)
	}
	if !strings.Contains(html, "zeta") {
		t.Errorf("the stale filter still narrowed the rows:\n%s", html)
	}
}

func TestSavedViewAnonymousCallerNoViews(t *testing.T) {
	x, _ := savedUI(t)
	// The strip does not draw for a caller the store would refuse, and
	// nothing about the anonymous request reaches the store as a user.
	html := listHTML(t, x.ui.List("orders").SavedViews(), x.ctx("/orders", ""))
	if strings.Contains(html, "Save view") {
		t.Errorf("an anonymous caller drew the save form:\n%s", html)
	}
}

// A caller the entity's read permission refuses saves nothing: the
// handler asks the list's own read gate before it parses a filter.
func TestSavedViewsNeedTheReadGate(t *testing.T) {
	cfg := ordersConfig()
	cfg.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}}
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": cfg},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}))
	store := newMemSavedViews()
	installOwnerExtractor(t)
	x.ui = x.ui.WithSavedViews(store)
	policy := access.NewRolePolicy()
	policy.Register("orders:read")
	r := httptest.NewRequest(http.MethodPost, "/api/orders/_views", strings.NewReader(url.Values{
		"name": {"Peek"}, "filter": {`status = "open"`}, "back": {"/orders"},
	}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = withUserRequest(r, "u1")
	r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"clerk"}))
	w := httptest.NewRecorder()
	savedViewsMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: a caller without orders:read saved a view: %d %s", w.Code, w.Body.String())
	}
	if views, _ := store.List(asUser(context.Background(), "u1"), "orders"); len(views) != 0 {
		t.Fatalf("SECURITY: the store holds %v", views)
	}
}

func TestSavedViewsCrossSiteRefused(t *testing.T) {
	x, _ := savedUI(t)
	r := httptest.NewRequest(http.MethodPost, "/api/orders/_views", strings.NewReader(url.Values{
		"name": {"Evil"}, "filter": {`status = "open"`}, "back": {"/orders"},
	}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r = withUserRequest(r, "u1")
	w := httptest.NewRecorder()
	savedViewsMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a cross-site save answered %d, want 403", w.Code)
	}
}

// An open saved view's filter is not in the query the export and
// every-match routes read, so the list offers neither: an admin would
// otherwise export or bulk-run past the view's narrowing. The same list
// with no view open offers both, so the refusal is the saved view's.
func TestSavedViewOffersNoExportOrEveryMatch(t *testing.T) {
	x, store := savedUI(t)
	saved, err := store.Create(asUser(x.ctx("/orders", ""), "u1"), SavedView{
		Entity: "orders", Name: "Any amount", Filter: "amount > 0",
	})
	if err != nil {
		t.Fatal(err)
	}
	list := func() *ListBuilder { return x.ui.List("orders").SavedViews().Bulk().PageSize(1) }
	plain := listHTML(t, list(), x.userCtx("/orders", "", "u1"))
	if !strings.Contains(plain, "_export.csv") || !strings.Contains(plain, `value="every"`) {
		t.Fatalf("the plain list offers no export or every-match, so the check is vacuous:\n%s", plain)
	}
	open := listHTML(t, list(), x.userCtx("/orders", "?saved="+saved.ID, "u1"))
	if strings.Contains(open, "_export.csv") {
		t.Errorf("SECURITY: an open saved view drew an Export link:\n%s", open)
	}
	if strings.Contains(open, `value="every"`) {
		t.Errorf("SECURITY: an open saved view offered every match:\n%s", open)
	}
}
