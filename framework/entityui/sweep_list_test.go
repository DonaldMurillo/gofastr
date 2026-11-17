package entityui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// A page size is capped by the entity's Pagination.MaxListLimit.
func TestSweepListPageSizeCappedByMaxListLimit(t *testing.T) {
	cfg := ordersConfig()
	cfg.Pagination = &entity.PaginationConfig{MaxListLimit: 1}
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": cfg}, map[string][]map[string]any{"orders": ordersRows()})
	html := listHTML(t, x.ui.List("orders").PageSize(5), x.ctx("/orders", ""))
	if strings.Contains(html, "alpha") == strings.Contains(html, "zeta") {
		t.Fatalf("want exactly one of two rows under MaxListLimit 1:\n%s", html)
	}
}

// The per-request list key registry drains once its requests are collected.
func TestSweepListKeyRegistryDrains(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	render := func() { _ = listHTML(t, x.ui.List("orders"), x.ctx("/orders", "")) }
	for range 20 {
		render()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		listKeyRegMu.Lock()
		n := len(listKeyReg)
		listKeyRegMu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d request entries still held after GC", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A rows-per-page menu with fewer than two sizes under MaxListLimit is not drawn.
func TestSweepListOneSizeOffersNoMenu(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Pagination = &entity.PaginationConfig{MaxListLimit: 30}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = nil
	for i := 1; i <= 40; i++ {
		rows["invoices"] = append(rows["invoices"], map[string]any{
			"id": fmt.Sprintf("inv-%02d", i), "number": fmt.Sprintf("INV-%02d", i), "status": "draft", "customer_id": "cus-1",
		})
	}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	h := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(h, "Rows per page") || !strings.Contains(h, "1–25 of 40") {
		t.Fatalf("one size on offer drew a menu, or lost the range:\n%s", h)
	}
}

// The empty state's clear link drops the search, filter and page, and keeps the rest of the URL.
func TestSweepListClearSearchDropsPage(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?q=nomatch&page=2&other=1"))
	if !strings.Contains(html, `href="/orders?other=1"`) {
		t.Fatalf("the clear link does not land on /orders?other=1:\n%s", html)
	}
}

// The toolbar's Reset is hidden while another list's state rides the URL, since Reset would drop it.
func TestSweepListResetHiddenUnderForeignParams(t *testing.T) {
	cfg := ordersConfig()
	cfg.Display = &entity.DisplayConfig{Facets: []string{"status"}}
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": cfg}, map[string][]map[string]any{"orders": ordersRows()})
	reset := ">" + i18nui.FilterReset.Text(context.Background()) + "<"
	if own := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?f_status=open")); !strings.Contains(own, reset) {
		t.Fatalf("setup: no Reset on a list holding only its own params:\n%s", own)
	}
	if foreign := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?f_status=open&due_page=2")); strings.Contains(foreign, reset) {
		t.Fatalf("Reset drawn while another list's param rides the URL:\n%s", foreign)
	}
}

func sweepHiddenViewUI(t *testing.T, show func(context.Context) bool, filt func(context.Context) (*filter.Predicate, error)) *testUI {
	t.Helper()
	ext := Extensions{Entities: map[string]Extension{"orders": {Views: map[string]ViewFunc{"open": {Filter: filt, Show: show}}}}}
	cfg := ordersConfig()
	cfg.Display = &entity.DisplayConfig{Views: []entity.ListView{{Key: "open"}}}
	return newTestUIExt(t, map[string]entity.EntityConfig{"orders": cfg}, map[string][]map[string]any{"orders": ordersRows()}, ext)
}

func sweepOpenOnly(context.Context) (*filter.Predicate, error) {
	return &filter.Predicate{Field: "status", Op: filter.OpEq, Value: "open"}, nil
}

// A URL naming a view its Show hides from the caller lists All, never that view's rows.
func TestSweepListURLHiddenViewShowsAll(t *testing.T) {
	x := sweepHiddenViewUI(t, func(context.Context) bool { return false }, sweepOpenOnly)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?view=open"))
	if !strings.Contains(html, "zeta") {
		t.Fatalf("?view= naming a hidden view applied it:\n%s", html)
	}
}

// A builder View naming no declared view fails the slot.
func TestSweepListBuilderUnknownViewFailsSlot(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	html := listHTML(t, x.ui.List("orders").View("nope"), x.ctx("/orders", ""))
	if strings.Contains(html, "alpha") || !strings.Contains(html, i18nui.EntitySlotFailedBody.Text(context.Background())) {
		t.Fatalf("an unknown builder View did not fail the slot:\n%s", html)
	}
}

// A view Filter runs as the caller: under a back office's elevation it cannot count rows its roles cannot read.
func TestSweepListViewFilterIsNotElevated(t *testing.T) {
	ran, leaked := false, false
	var x *testUI
	ext := Extensions{Entities: map[string]Extension{"invoices": {Views: map[string]ViewFunc{"mine": {Filter: func(ctx context.Context) (*filter.Predicate, error) {
		ran = true
		if _, ok := x.ui.Count(ctx, "invoices", ""); ok {
			leaked = true
		}
		return nil, nil
	}}}}}}
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
	inv.Display.Views = []entity.ListView{{Key: "mine"}}
	ents["invoices"] = inv
	x = newTestUIExt(t, ents, invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	policy.Register("invoices:read")
	x.ui.List("invoices").Base("/invoices").RenderCtx(elevatedClerk(x, policy, "/invoices", "?view=mine"))
	if !ran {
		t.Fatal("setup: the elevated list never ran the view's Filter")
	}
	if leaked {
		t.Fatal("SECURITY: a view Filter counted rows with the back office's elevation")
	}
}

// The Deleted view draws Delete permanently only for a caller who may delete the row.
func TestSweepListPurgeNeedsDeleteToDraw(t *testing.T) {
	x, policy := ownerNotesUI(t)
	if err := policy.Grant("updater", "notes:update"); err != nil {
		t.Fatal(err)
	}
	ctx := access.WithRoles(access.WithPolicy(x.userCtx("/notes", "?view=deleted", "u1"), policy), []string{"updater"})
	html := listHTML(t, x.ui.List("notes").Deleted(), ctx)
	if !strings.Contains(html, "_restore") || strings.Contains(html, "_purge") {
		t.Fatalf("want Restore and no Delete permanently for an updater:\n%s", html)
	}
}

// A trash write from a caller who may not read the entity answers the list route's 403 and writes nothing.
func TestSweepListTrashWriteNeedsRead(t *testing.T) {
	cfg := notesConfig()
	cfg.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "notes:read", Update: "notes:update", Delete: "notes:delete"}}
	x := newTestUI(t, map[string]entity.EntityConfig{"notes": cfg},
		map[string][]map[string]any{"notes": {{"id": "n1", "name": "trashed", "status": "open", "deleted_at": "2026-01-01T00:00:00Z"}}},
		withAPI(map[string]string{"notes": "/api/notes"}), withSingleConn())
	policy := access.NewRolePolicy()
	policy.Register("notes:read", "notes:update", "notes:delete")
	if err := policy.Grant("writer", "notes:update", "notes:delete"); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"_restore", "_purge"} {
		r := httptest.NewRequest(http.MethodPost, "/api/notes/n1/"+op, strings.NewReader(url.Values{"back": {"/notes?view=deleted"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r = withUserRequest(r, "u1")
		r = r.WithContext(access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"writer"}))
		w := httptest.NewRecorder()
		deletedWriteMux(x).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s without read answered %d, want the list route's 403", op, w.Code)
		}
	}
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'n1' AND deleted_at IS NOT NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("a refused trash write changed the row (n=%d err=%v)", n, err)
	}
}

// An anonymous caller with a filter on the URL gets no Save view form.
func TestSweepListAnonymousGetsNoSaveForm(t *testing.T) {
	x, _ := savedUI(t)
	q := "?" + url.Values{"filter": {`status = "open"`}}.Encode()
	if signed := listHTML(t, x.ui.List("orders").SavedViews(), x.userCtx("/orders", q, "u1")); !strings.Contains(signed, "Save view") {
		t.Fatalf("setup: a signed-in caller got no save form:\n%s", signed)
	}
	if anon := listHTML(t, x.ui.List("orders").SavedViews(), x.ctx("/orders", q)); strings.Contains(anon, "Save view") {
		t.Fatalf("an anonymous caller drew the save form:\n%s", anon)
	}
}

// A save from a caller with no user answers 403 naming sign-in, and stores nothing.
func TestSweepListAnonymousSaveRefused(t *testing.T) {
	x, store := savedUI(t)
	r := httptest.NewRequest(http.MethodPost, "/api/orders/_views", strings.NewReader(url.Values{"name": {"v"}, "back": {"/orders"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	savedViewsMux(x).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), reasonViewSignIn.Code()) {
		t.Fatalf("anonymous save = %d %s, want 403 %s", w.Code, w.Body.String(), reasonViewSignIn.Code())
	}
	if views, _ := store.List(x.userCtx("/orders", "", "u1"), "orders"); len(views) != 0 {
		t.Fatalf("an anonymous save stored %v", views)
	}
}

// Filter rows past maxFilterRows are not read.
func TestSweepListFilterRowsCapped(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	q := url.Values{}
	for range maxFilterRows {
		q.Add("rf", "name")
		q.Add("ro", "!=")
		q.Add("rv", "nobody")
	}
	q.Add("rf", "name")
	q.Add("ro", "=")
	q.Add("rv", "nobody")
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?"+q.Encode()))
	if !strings.Contains(html, "alpha") {
		t.Fatalf("a filter row past maxFilterRows was applied:\n%s", html)
	}
}

// The Deleted view draws no bulk bar.
func TestSweepListDeletedViewHasNoBulkBar(t *testing.T) {
	x := deletedUI(t)
	html := listHTML(t, x.ui.List("notes").Deleted().Bulk(), x.userCtx("/notes", "?view=deleted", "u1"))
	if strings.Contains(html, "/_bulk") {
		t.Fatalf("the trash view drew a bulk bar:\n%s", html)
	}
}

// An undecodable f_ param naming no field of the list is not the list's warning.
func TestSweepListForeignFacetPairNoWarning(t *testing.T) {
	x := newTestUI(t, map[string]entity.EntityConfig{"orders": ordersConfig()}, map[string][]map[string]any{"orders": ordersRows()})
	if got := listHTML(t, x.ui.List("orders").QueryBox(), x.ctx("/orders", "?f_nofield=%ZZ")); strings.Contains(got, "Filter not applied") {
		t.Fatalf("a foreign f_ pair warned this list:\n%s", got)
	}
}

// A return path carrying a control byte is refused before it reaches a Location header.
func TestSweepListBackPathRefusesControlBytes(t *testing.T) {
	for _, bad := range []string{"/notes\x01", "/notes\x7f", "/notes?\x1bx"} {
		if _, ok := scrubBackPath(bad); ok {
			t.Errorf("scrubBackPath accepted %q", bad)
		}
	}
	if _, ok := scrubBackPath("/notes?view=deleted"); !ok {
		t.Fatal("scrubBackPath refused a plain path")
	}
}
