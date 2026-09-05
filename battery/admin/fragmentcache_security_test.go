package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the admin fragment cache-suppression gap, found by the 2026-09-04
// red-probe round; fixed in Battery.gate by stamping Cache-Control:
// no-store once at the choke point every admin-owned response passes
// through.
// Property: every admin response that carries row data must suppress
// shared/browser caching (Cache-Control: no-store), the posture writePage
// already pins for the standalone pages and the uihost pins for every SSR
// screen - an intermediary or the back/forward cache must never be able to
// retain one admin's row fragment.
// Surfaces: battery/admin/admin.go Battery.gate (stamps no-store before
// the auth decision, so refusals carry it too),
// battery/admin/entity_admin.go entityRows (GET /admin/e/<t>/_rows),
// battery/admin/entity_admin.go entityDelete (DELETE /admin/e/<t>/_delete/
// {id}, returns the refreshed row-data table fragment),
// battery/admin/admin.go writePage (standalone pages, pinned before),
// battery/admin/admin.go handleCSS (deliberately ungated, keeps its public
// max-age: it carries no data and lets the 401 page degrade gracefully).
func TestAdminFragmentsCarryNoStore(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"posts": postsConfig()})
	base := mountAdminBattery(t, app, Config{Entities: []string{"posts"}})
	h := asUser(base, testUser{"u1"})

	// Seed TWO rows: one is the delete target, the other survives so the
	// refreshed delete fragment demonstrably still carries row data (the
	// endpoint deletes then re-renders the table, so the deleted row itself
	// can never be the fragment's payload).
	for _, title := range []string{"hello", "survivor"} {
		if rr := postForm(h, "/admin/e/posts/_create", url.Values{"title": {title}, "body": {"world"}}); rr.Code != http.StatusSeeOther {
			t.Fatalf("create %q: %d body=%s", title, rr.Code, rr.Body.String())
		}
	}
	// Delete the "hello" row by its own id: firstID's LIMIT 1 has no
	// guaranteed order under uuid PKs, and the sanity assertion below
	// needs to know exactly which row survived.
	var id string
	if err := db.QueryRow(`SELECT id FROM posts WHERE title = 'hello'`).Scan(&id); err != nil {
		t.Fatalf("seeded row lookup: %v", err)
	}

	surfaces := []struct {
		name string
		want string
		do   func() *httptest.ResponseRecorder
	}{
		{"rows fragment", "hello", func() *httptest.ResponseRecorder { return get(h, "/admin/e/posts/_rows") }},
		{"delete fragment", "survivor", func() *httptest.ResponseRecorder { return del(h, "/admin/e/posts/_delete/"+id) }},
		{"standalone page", "Overview", func() *httptest.ResponseRecorder { return get(h, "/admin") }},
	}
	for _, s := range surfaces {
		rr := s.do()
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d body=%s", s.name, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), s.want) {
			t.Fatalf("%s: sanity - fragment does not carry the expected row/page data %q", s.name, s.want)
		}
		if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Fatalf("SECURITY: [admin-cache] %s returns data with Cache-Control %q - a data-bearing admin response must be no-store like every writePage/uihost surface", s.name, cc)
		}
	}

	// The stamp lives at the gate, ahead of the auth decision, so even the
	// refusal (and the login redirect, when configured) is uncacheable.
	anon := asUser(base, nil)
	rr := get(anon, "/admin")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anon /admin: status %d, want 401", rr.Code)
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("SECURITY: [admin-cache] gate refusal carries Cache-Control %q - the no-store stamp must precede the auth decision", cc)
	}
}
