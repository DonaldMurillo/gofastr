package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The kanban demo's move handler renews the board's version on every
// 204, and its conflict endpoint returns the version its rows were read
// at, so a page that applies the header saves move after move and a
// stale token still conflicts.
func TestSortableMoveRenewsVersion(t *testing.T) {
	app := setupServer()
	var cookie *http.Cookie
	send := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		for _, got := range rec.Result().Cookies() {
			if got.Name == demoCookieName {
				cookie = got
			}
		}
		return rec
	}
	const header = "X-Gofastr-Sortable-Version"
	for step, move := range []struct{ order, version, renewed string }{
		{"k2,k1", "v1", "v2"},
		{"k1,k2", "v2", "v3"},
		{"k2,k1", "v3", "v4"},
	} {
		rec := send(http.MethodPost, "/__site/sortable/move", "container=todo&order="+move.order+"&version="+move.version)
		if rec.Code != http.StatusNoContent || rec.Header().Get(header) != move.renewed {
			t.Fatalf("move %d: status %d, %s %q; want 204 and %q", step+1, rec.Code, header, rec.Header().Get(header), move.renewed)
		}
	}
	if rec := send(http.MethodPost, "/__site/sortable/move", "container=todo&order=k1,k2&version=v1"); rec.Code != http.StatusConflict || rec.Header().Get(header) != "" {
		t.Fatalf("stale move: status %d, %s %q; want 409 and no renewal", rec.Code, header, rec.Header().Get(header))
	}
	rec := send(http.MethodGet, "/__site/sortable/conflict?container=todo", "")
	if rec.Code != http.StatusOK || rec.Header().Get(header) != "v4" {
		t.Fatalf("conflict refresh: status %d, %s %q; want 200 and v4", rec.Code, header, rec.Header().Get(header))
	}
	if body := rec.Body.String(); strings.Index(body, `data-hui-sort-key="k2"`) > strings.Index(body, `data-hui-sort-key="k1"`) {
		t.Fatalf("conflict refresh rows are not the saved order k2,k1: %s", body)
	}
}
